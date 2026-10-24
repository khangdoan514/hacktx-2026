import threading
from uuid import uuid4

import psycopg
import pytest
from fastapi.testclient import TestClient
from pydantic import ValidationError

from app.core.auth import encode_token
from app.main import app

DSN = "postgresql://ubuntu@/hacktx_test"

client = TestClient(app)


def auth(user_id, email) -> dict:
    return {"Authorization": f"Bearer {encode_token(user_id, email)}"}


def make_user(email: str, name: str | None = None) -> tuple[str, dict]:
    user_id = str(uuid4())
    with psycopg.connect(DSN) as conn:
        conn.execute("INSERT INTO auth.users (id, email) VALUES (%s, %s)", (user_id, email))
        conn.execute(
            "INSERT INTO app.profiles (id, display_name) VALUES (%s, %s)",
            (user_id, name or email.split("@")[0]),
        )
        conn.commit()
    return user_id, auth(user_id, email)


def workspace(headers, name="NMC3") -> str:
    response = client.post("/api/v1/workspaces", headers=headers, json={"name": name, "timezone": "UTC"})
    assert response.status_code == 201, response.text
    return response.json()["id"]


def project(headers, workspace_id, name="Capacity", project_type="internal", **extra) -> dict:
    body = {"name": name, "project_type": project_type, **extra}
    response = client.post(f"/api/v1/workspaces/{workspace_id}/projects", headers=headers, json=body)
    assert response.status_code == 201, response.text
    return response.json()


def test_missing_and_invalid_tokens():
    assert client.get("/api/v1/me").status_code == 401
    assert client.get("/api/v1/me", headers={"Authorization": "Bearer not-a-token"}).status_code == 401
    owner, headers = make_user("owner@example.com")
    stale = encode_token(owner, "owner@example.com")
    # Tamper the signature.
    assert client.get("/api/v1/me", headers={"Authorization": f"Bearer {stale}extra"}).status_code == 401


def test_local_auth_and_distinct_memberships():
    created = client.post(
        "/api/v1/auth/local-session",
        json={"email": "ada@example.com", "password": "correct-horse", "create": True, "display_name": "Ada"},
    )
    assert created.status_code == 200, created.text
    denied = client.post(
        "/api/v1/auth/local-session",
        json={"email": "ada@example.com", "password": "wrong-password-1"},
    )
    assert denied.status_code == 401
    ada = {"Authorization": f"Bearer {created.json()['access_token']}"}
    grace = client.post(
        "/api/v1/auth/local-session",
        json={"email": "grace@example.com", "password": "correct-horse", "create": True},
    )
    grace_headers = {"Authorization": f"Bearer {grace.json()['access_token']}"}
    ada_workspace = workspace(ada, "Ada Co")
    grace_workspace = workspace(grace_headers, "Grace Co")
    assert client.get(f"/api/v1/workspaces/{grace_workspace}", headers=ada).status_code == 404
    assert client.get(f"/api/v1/workspaces/{ada_workspace}", headers=grace_headers).status_code == 404


def test_workspace_and_partner_isolation():
    owner, owner_headers = make_user("owner@example.com", "Owner")
    partner_a, partner_a_headers = make_user("a@partner.test", "Partner A")
    partner_b, partner_b_headers = make_user("b@partner.test", "Partner B")
    workspace_id = workspace(owner_headers)
    other_owner, other_headers = make_user("other@example.com", "Other")
    other_workspace = workspace(other_headers, "Other Co")
    partner_org_a = client.post(
        f"/api/v1/workspaces/{workspace_id}/partners",
        headers=owner_headers,
        json={"name": "Partner A"},
    ).json()["id"]
    partner_org_b = client.post(
        f"/api/v1/workspaces/{workspace_id}/partners",
        headers=owner_headers,
        json={"name": "Partner B"},
    ).json()["id"]
    client.post(
        f"/api/v1/partners/{partner_org_a}/contacts",
        headers=owner_headers,
        json={"name": "A", "email": "a@partner.test"},
    )
    client.post(
        f"/api/v1/partners/{partner_org_b}/contacts",
        headers=owner_headers,
        json={"name": "B", "email": "b@partner.test"},
    )
    project_a = project(
        owner_headers,
        workspace_id,
        "Shared A",
        "partner",
        partner_id=partner_org_a,
        internal_notes="secret A",
        internal_cost="42.50",
    )
    project_b = project(
        owner_headers,
        workspace_id,
        "Shared B",
        "partner",
        partner_id=partner_org_b,
        internal_notes="secret B",
        internal_cost="99",
    )
    hidden = project(other_headers, other_workspace, "Hidden")
    for user_id, email, project_id, org_id in (
        (partner_a, "a@partner.test", project_a["id"], partner_org_a),
        (partner_b, "b@partner.test", project_b["id"], partner_org_b),
    ):
        invited = client.post(
            f"/api/v1/workspaces/{workspace_id}/invitations",
            headers=owner_headers,
            json={
                "email": email,
                "access_role": "guest",
                "project_id": project_id,
                "project_role": "partner",
                "partner_id": org_id,
            },
        )
        assert invited.status_code == 201, invited.text
        accepted = client.post(f"/api/v1/invitations/{invited.json()['token']}/accept", headers=auth(user_id, email))
        assert accepted.status_code == 200, accepted.text

    seen = client.get(f"/api/v1/projects/{project_a['id']}", headers=partner_a_headers)
    assert seen.status_code == 200, seen.text
    assert "internal_cost" not in seen.json()
    assert "internal_notes" not in seen.json()
    assert client.get(f"/api/v1/projects/{project_b['id']}", headers=partner_a_headers).status_code == 404
    assert client.get(f"/api/v1/projects/{hidden['id']}", headers=owner_headers).status_code == 404
    partner_list = client.get(f"/api/v1/workspaces/{workspace_id}/partners", headers=partner_a_headers)
    assert partner_list.status_code == 200
    assert [item["name"] for item in partner_list.json()["items"]] == ["Partner A"]

    with psycopg.connect(DSN) as conn:
        with conn.transaction():
            conn.execute("SET LOCAL ROLE app_runtime")
            conn.execute("SELECT set_config('app.user_id', %s, true)", (partner_a,))
            conn.execute("SELECT set_config('app.user_email', 'a@partner.test', true)")
            rows = conn.execute("SELECT project_id FROM app.project_internal").fetchall()
            assert rows == []
        with conn.transaction():
            conn.execute("SET LOCAL ROLE app_runtime")
            conn.execute("SELECT set_config('app.user_id', %s, true)", (owner,))
            conn.execute("SELECT set_config('app.user_email', 'owner@example.com', true)")
            visible = conn.execute("SELECT id FROM app.projects").fetchall()
            assert hidden["id"] not in {str(row[0]) for row in visible}
        with conn.transaction():
            conn.execute("SET LOCAL ROLE app_runtime")
            conn.execute("SELECT set_config('app.user_id', %s, true)", (other_owner,))
            conn.execute("SELECT set_config('app.user_email', 'other@example.com', true)")
            visible = conn.execute("SELECT id FROM app.projects").fetchall()
            assert project_a["id"] not in {str(row[0]) for row in visible}


def test_functional_role_does_not_grant_project_access():
    _owner, owner_headers = make_user("owner@example.com")
    analyst, analyst_headers = make_user("analyst@example.com")
    workspace_id = workspace(owner_headers)
    # Invite the analyst as a workspace member with no project grant.
    invited = client.post(
        f"/api/v1/workspaces/{workspace_id}/invitations",
        headers=owner_headers,
        json={"email": "analyst@example.com", "access_role": "member"},
    )
    assert invited.status_code == 201, invited.text
    assert client.post(f"/api/v1/invitations/{invited.json()['token']}/accept", headers=analyst_headers).status_code == 200
    client.post(
        f"/api/v1/workspaces/{workspace_id}/functions",
        headers=owner_headers,
        json={"user_id": analyst, "code": "dba"},
    )
    created = project(owner_headers, workspace_id, "Confidential", internal_notes="keep out")
    assert client.get(f"/api/v1/projects/{created['id']}", headers=analyst_headers).status_code == 404


def test_invitation_replay_wrong_identity_and_race():
    owner, owner_headers = make_user("owner@example.com")
    guest, guest_headers = make_user("guest@example.com")
    stranger, stranger_headers = make_user("stranger@example.com")
    workspace_id = workspace(owner_headers)
    invited = client.post(
        f"/api/v1/workspaces/{workspace_id}/invitations",
        headers=owner_headers,
        json={"email": "guest@example.com", "access_role": "member"},
    )
    token = invited.json()["token"]
    assert client.post(f"/api/v1/invitations/{token}/accept", headers=stranger_headers).status_code == 404
    results = []

    def accept():
        response = client.post(f"/api/v1/invitations/{token}/accept", headers=guest_headers)
        results.append(response.status_code)

    threads = [threading.Thread(target=accept), threading.Thread(target=accept)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()
    assert results == [200, 200]
    again = client.post(f"/api/v1/invitations/{token}/accept", headers=guest_headers)
    assert again.status_code == 200
    with psycopg.connect(DSN) as conn:
        count = conn.execute(
            "SELECT count(*) FROM app.workspace_memberships WHERE workspace_id = %s AND user_id = %s",
            (workspace_id, guest),
        ).fetchone()[0]
        assert count == 1
    again = client.post(f"/api/v1/invitations/{token}/accept", headers=guest_headers)
    assert again.status_code == 200


def test_last_owner_cannot_be_removed():
    owner, owner_headers = make_user("owner@example.com")
    workspace_id = workspace(owner_headers)
    removed = client.patch(
        f"/api/v1/workspaces/{workspace_id}/members/{owner}",
        headers=owner_headers,
        json={"status": "removed"},
    )
    assert removed.status_code == 409
    assert removed.json()["code"] == "last_owner"


def test_request_convert_is_idempotent():
    owner, owner_headers = make_user("owner@example.com")
    workspace_id = workspace(owner_headers)
    created = client.post(
        f"/api/v1/workspaces/{workspace_id}/requests",
        headers=owner_headers,
        json={"title": "Internal ask", "outcome": "Ship it"},
    )
    request_id = created.json()["id"]
    client.post(f"/api/v1/requests/{request_id}/submit", headers=owner_headers, json={"expected_version": 1})
    client.post(f"/api/v1/requests/{request_id}/triage", headers=owner_headers, json={"expected_version": 2})
    client.post(f"/api/v1/requests/{request_id}/accept", headers=owner_headers, json={"expected_version": 3})
    headers = {**owner_headers, "Idempotency-Key": "convert-1"}
    first = client.post(f"/api/v1/requests/{request_id}/convert", headers=headers)
    second = client.post(f"/api/v1/requests/{request_id}/convert", headers=headers)
    assert first.status_code == 200, first.text
    assert second.status_code == 200, second.text
    assert first.json()["project_id"] == second.json()["project_id"]
    with psycopg.connect(DSN) as conn:
        count = conn.execute("SELECT count(*) FROM app.projects").fetchone()[0]
        assert count == 1


def test_task_cycle_and_stale_edit():
    owner, headers = make_user("owner@example.com")
    workspace_id = workspace(headers)
    created = project(headers, workspace_id)
    first = client.post(
        f"/api/v1/projects/{created['id']}/tasks",
        headers=headers,
        json={"title": "One", "acceptance_criteria": "done"},
    ).json()
    second = client.post(
        f"/api/v1/projects/{created['id']}/tasks",
        headers=headers,
        json={"title": "Two"},
    ).json()
    assert client.post(
        f"/api/v1/tasks/{first['id']}/dependencies",
        headers=headers,
        json={"depends_on": second["id"]},
    ).status_code == 201
    cycle = client.post(
        f"/api/v1/tasks/{second['id']}/dependencies",
        headers=headers,
        json={"depends_on": first["id"]},
    )
    assert cycle.status_code == 422
    assert cycle.json()["code"] == "task_cycle"
    stale = client.patch(
        f"/api/v1/tasks/{first['id']}",
        headers=headers,
        json={"expected_version": 99, "title": "Changed"},
    )
    assert stale.status_code == 409


def test_approval_self_and_concurrent():
    owner, owner_headers = make_user("owner@example.com")
    reviewer, reviewer_headers = make_user("reviewer@example.com")
    workspace_id = workspace(owner_headers)
    invited = client.post(
        f"/api/v1/workspaces/{workspace_id}/invitations",
        headers=owner_headers,
        json={"email": "reviewer@example.com", "access_role": "member"},
    )
    client.post(f"/api/v1/invitations/{invited.json()['token']}/accept", headers=reviewer_headers)
    created = project(owner_headers, workspace_id)
    client.post(
        f"/api/v1/projects/{created['id']}/members",
        headers=owner_headers,
        json={"user_id": reviewer, "access_role": "reviewer"},
    )
    requirement = client.post(
        f"/api/v1/projects/{created['id']}/requirements",
        headers=owner_headers,
        json={"title": "Capacity requirement", "body": "Add capacity"},
    )
    assert requirement.status_code == 201, requirement.text
    submitted = client.post(
        f"/api/v1/requirements/{requirement.json()['id']}/submit-review",
        headers=owner_headers,
        json={"expected_version": 1},
    )
    assert submitted.status_code == 200, submitted.text
    approval = client.post(
        f"/api/v1/projects/{created['id']}/approvals",
        headers=owner_headers,
        json={
            "target_type": "requirement",
            "target_id": requirement.json()["id"],
            "target_revision": 1,
            "reviewer_id": reviewer,
        },
    )
    assert approval.status_code == 201, approval.text
    approval_id = approval.json()["id"]
    self_approve = client.post(
        f"/api/v1/approvals/{approval_id}/approve",
        headers=owner_headers,
        json={"expected_version": 1, "target_revision": 1, "comment": "no"},
    )
    assert self_approve.status_code == 403
    body = {"expected_version": 1, "target_revision": 1, "comment": "yes"}
    results = []

    def decide():
        response = client.post(f"/api/v1/approvals/{approval_id}/approve", headers=reviewer_headers, json=body)
        results.append(response.status_code)

    threads = [threading.Thread(target=decide), threading.Thread(target=decide)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()
    assert sorted(results) == [200, 409]
    fetched = client.get(f"/api/v1/requirements/{requirement.json()['id']}", headers=owner_headers)
    assert fetched.json()["state"] == "accepted"
    assert fetched.json()["accepted_revision"] == 1
    with psycopg.connect(DSN) as conn:
        decisions = conn.execute(
            "SELECT count(*) FROM app.approval_decisions WHERE approval_id = %s",
            (approval_id,),
        ).fetchone()[0]
        events = conn.execute(
            "SELECT count(*) FROM app.outbox_events WHERE resource_id = %s AND kind = 'approval.approved'",
            (approval_id,),
        ).fetchone()[0]
    assert decisions == 1
    assert events == 1


def test_approval_rolls_back_when_target_changes():
    owner, owner_headers = make_user("owner2@example.com")
    reviewer, reviewer_headers = make_user("reviewer2@example.com")
    workspace_id = workspace(owner_headers)
    invited = client.post(
        f"/api/v1/workspaces/{workspace_id}/invitations",
        headers=owner_headers,
        json={"email": "reviewer2@example.com", "access_role": "member"},
    )
    client.post(f"/api/v1/invitations/{invited.json()['token']}/accept", headers=reviewer_headers)
    created = project(owner_headers, workspace_id)
    client.post(
        f"/api/v1/projects/{created['id']}/members",
        headers=owner_headers,
        json={"user_id": reviewer, "access_role": "reviewer"},
    )
    requirement = client.post(
        f"/api/v1/projects/{created['id']}/requirements",
        headers=owner_headers,
        json={"title": "Editable", "body": "First"},
    ).json()
    client.post(
        f"/api/v1/requirements/{requirement['id']}/submit-review",
        headers=owner_headers,
        json={"expected_version": 1},
    )
    approval = client.post(
        f"/api/v1/projects/{created['id']}/approvals",
        headers=owner_headers,
        json={
            "target_type": "requirement",
            "target_id": requirement["id"],
            "target_revision": 1,
            "reviewer_id": reviewer,
        },
    ).json()
    revised = client.post(
        f"/api/v1/requirements/{requirement['id']}/revisions",
        headers=owner_headers,
        json={"expected_version": 2, "body": "Changed during review", "acceptance_criteria": "still required"},
    )
    assert revised.status_code == 201, revised.text
    denied = client.post(
        f"/api/v1/approvals/{approval['id']}/approve",
        headers=reviewer_headers,
        json={"expected_version": 1, "target_revision": 1, "comment": "too late"},
    )
    assert denied.status_code == 409
    fetched = client.get(f"/api/v1/requirements/{requirement['id']}", headers=owner_headers)
    assert fetched.json()["state"] == "in_review"
    with psycopg.connect(DSN) as conn:
        decisions = conn.execute(
            "SELECT count(*) FROM app.approval_decisions WHERE approval_id = %s",
            (approval["id"],),
        ).fetchone()[0]
        approved_events = conn.execute(
            "SELECT count(*) FROM app.outbox_events WHERE resource_id = %s AND kind = 'approval.approved'",
            (approval["id"],),
        ).fetchone()[0]
    assert decisions == 0
    assert approved_events == 0


def test_runtime_role_and_connection_isolation():
    with psycopg.connect(DSN) as conn:
        bypass = conn.execute("SELECT rolbypassrls FROM pg_roles WHERE rolname = 'app_runtime'").fetchone()[0]
        owners = conn.execute(
            """
            SELECT DISTINCT pg_get_userbyid(cls.relowner)
            FROM pg_class cls
            JOIN pg_namespace namespace ON namespace.oid = cls.relnamespace
            WHERE namespace.nspname = 'app' AND cls.relkind = 'r'
            """
        ).fetchall()
    assert bypass is False
    assert owners
    assert all(row[0] != "app_runtime" for row in owners)
    owner, owner_headers = make_user("pool-a@example.com")
    other, other_headers = make_user("pool-b@example.com")
    workspace_a = workspace(owner_headers, "Pool A")
    hidden = project(owner_headers, workspace_a, "Secret")
    workspace(other_headers, "Pool B")
    conn = psycopg.connect(DSN)
    try:
        with conn.transaction():
            conn.execute("SET LOCAL ROLE app_runtime")
            conn.execute("SELECT set_config('app.user_id', %s, true)", (owner,))
            conn.execute("SELECT set_config('app.user_email', 'pool-a@example.com', true)")
            visible = {str(row[0]) for row in conn.execute("SELECT id FROM app.projects").fetchall()}
            assert hidden["id"] in visible
        with conn.transaction():
            conn.execute("SET LOCAL ROLE app_runtime")
            conn.execute("SELECT set_config('app.user_id', %s, true)", (other,))
            conn.execute("SELECT set_config('app.user_email', 'pool-b@example.com', true)")
            visible = {str(row[0]) for row in conn.execute("SELECT id FROM app.projects").fetchall()}
            assert hidden["id"] not in visible
    finally:
        conn.close()


def test_local_auth_refused_in_production():
    from app.core.config import Settings

    with pytest.raises(ValidationError):
        Settings(allow_local_auth=True, environment="production")


def test_handoff_snapshot_is_immutable():
    owner, owner_headers = make_user("owner@example.com")
    recipient, recipient_headers = make_user("recipient@example.com")
    workspace_id = workspace(owner_headers)
    invited = client.post(
        f"/api/v1/workspaces/{workspace_id}/invitations",
        headers=owner_headers,
        json={"email": "recipient@example.com", "access_role": "member"},
    )
    client.post(f"/api/v1/invitations/{invited.json()['token']}/accept", headers=recipient_headers)
    created = project(owner_headers, workspace_id)
    client.post(
        f"/api/v1/projects/{created['id']}/members",
        headers=owner_headers,
        json={"user_id": recipient, "access_role": "contributor"},
    )
    task = client.post(
        f"/api/v1/projects/{created['id']}/tasks",
        headers=owner_headers,
        json={"title": "Original title"},
    ).json()
    handoff = client.post(f"/api/v1/projects/{created['id']}/handoffs", headers=owner_headers)
    assert handoff.status_code == 201, handoff.text
    sent = client.post(
        f"/api/v1/handoffs/{handoff.json()['id']}/send",
        headers=owner_headers,
        json={
            "expected_version": 1,
            "objective": "Explain margin",
            "requested_output": "A decision",
            "recipient_id": recipient,
            "source_references": [{"type": "task", "id": task["id"], "title": "Original title"}],
        },
    )
    assert sent.status_code == 200, sent.text
    client.patch(
        f"/api/v1/tasks/{task['id']}",
        headers=owner_headers,
        json={"expected_version": 1, "title": "Renamed"},
    )
    fetched = client.get(f"/api/v1/handoffs/{handoff.json()['id']}", headers=recipient_headers)
    assert fetched.json()["revisions"][0]["snapshot"]["source_references"][0]["title"] == "Original title"
    assert fetched.json()["state"] == "sent"


def test_calculation_dashboard_and_documents():
    owner, headers = make_user("owner@example.com")
    workspace_id = workspace(headers)
    created = project(headers, workspace_id, "Capacity expansion")
    dataset = client.post(
        f"/api/v1/workspaces/{workspace_id}/datasets",
        headers=headers,
        json={"name": "Capacity", "description": "Demo rows"},
    ).json()
    csv_text = "\n".join(
        [
            "partner_id,contract_id,contract_status,contract_end,period,resource,available_capacity,committed_capacity,consumed_capacity,gpu_hours,revenue,included_cost,currency,extra_allocation",
            "p1,c1,active,2027-06-01,2026-Q4,gpu,100,40,30,30,1000,400,USD,10",
            "p2,c2,active,2027-06-01,2026-Q4,gpu,100,40,30,30,500,,USD,10",
            "p3,c3,active,2026-11-01,2026-Q4,gpu,100,40,30,30,800,100,USD,50",
        ]
    )
    imported = client.post(f"/api/v1/datasets/{dataset['id']}/imports", headers=headers, json={"csv": csv_text})
    assert imported.status_code == 201, imported.text
    assert imported.json()["summary"]["quarantined"] == 1
    metric = client.post(
        f"/api/v1/workspaces/{workspace_id}/metrics",
        headers=headers,
        json={"name": "Gross margin"},
    ).json()
    version = client.post(
        f"/api/v1/metrics/{metric['id']}/versions",
        headers=headers,
        json={"formula": "gross_margin", "period": "2026-Q4", "state": "draft"},
    ).json()
    accepted = client.post(f"/api/v1/metric-versions/{version['id']}/accept", headers=headers)
    assert accepted.status_code == 200, accepted.text
    analysis = client.post(
        f"/api/v1/projects/{created['id']}/analyses",
        headers=headers,
        json={"title": "Expansion", "question": "Who can take more capacity?"},
    ).json()
    calculated = client.post(
        f"/api/v1/analyses/{analysis['id']}/calculate",
        headers=headers,
        json={
            "dataset_version_id": imported.json()["id"],
            "metric_version_id": version["id"],
            "target_margin": "0.3",
            "forecast_period": "2026-Q4",
            "exclude_contract_end_before": "2027-01-01",
        },
    )
    assert calculated.status_code == 200, calculated.text
    output = calculated.json()["output"]
    by_partner = {item["partner_id"]: item for item in output["partners"]}
    excluded = {item["partner_id"]: item["reason"] for item in output["excluded"]}
    assert by_partner["p1"]["eligible"] is True
    assert by_partner["p1"]["margin"] == "0.6"
    assert excluded["p2"] == "missing_cost"
    assert excluded["p3"] == "contract_expiring"
    assert output["capacity_shortfall"] is False
    assert output["requested_allocation"] == "10"
    again = client.post(
        f"/api/v1/analyses/{analysis['id']}/calculate",
        headers=headers,
        json={
            "dataset_version_id": imported.json()["id"],
            "metric_version_id": version["id"],
            "target_margin": "0.3",
            "forecast_period": "2026-Q4",
            "exclude_contract_end_before": "2027-01-01",
        },
    )
    assert again.json()["output"] == output
    definition = {
        "schema_version": 1,
        "title": "Margin",
        "widgets": [{"type": "metric_number", "metric_version_id": version["id"]}],
    }
    dashboard = client.post(
        f"/api/v1/projects/{created['id']}/dashboards",
        headers=headers,
        json={"title": "Manual", "definition": definition},
    )
    assert dashboard.status_code == 201, dashboard.text
    assert dashboard.json()["revision"]["provenance"]["actor_type"] == "human"
    assert dashboard.json()["context_manifest"]["restrictions"] == ["current_project_membership"]
    published = client.post(
        f"/api/v1/dashboard-revisions/{dashboard.json()['revision']['id']}/publish",
        headers=headers,
    )
    assert published.status_code == 200, published.text
    assert published.json()["state"] == "published"
    unsafe = client.post(
        f"/api/v1/dashboards/{dashboard.json()['id']}/revisions",
        headers=headers,
        json={"base_revision": 1, "definition": {"schema_version": 1, "widgets": [{"type": "script"}]}},
    )
    assert unsafe.status_code == 422
    claimed = client.post(
        f"/api/v1/projects/{created['id']}/dashboards",
        headers=headers,
        json={"title": "Claimed", "definition": {**definition, "actor_type": "integration"}},
    )
    assert claimed.status_code == 422
    assert claimed.json()["code"] == "forbidden_actor"
    stale = client.post(
        f"/api/v1/dashboards/{dashboard.json()['id']}/revisions",
        headers=headers,
        json={"base_revision": 0, "definition": definition},
    )
    assert stale.status_code == 409
    restricted = client.post(
        f"/api/v1/projects/{created['id']}/dashboards",
        headers=headers,
        json={"title": "Restricted", "definition": {**definition, "restrictions": ["other-workspace"]}},
    )
    assert restricted.status_code == 201, restricted.text
    assert "other-workspace" not in restricted.json()["context_manifest"]["restrictions"]
    assert client.get(f"/api/v1/projects/{created['id']}/assistance-requests", headers=headers).status_code == 404
    document = client.post(
        f"/api/v1/projects/{created['id']}/documents",
        headers=headers,
        json={"title": "Evidence", "filename": "note.txt", "mime_type": "text/plain"},
    ).json()
    version_id = document["version"]["id"]
    assert client.post(
        f"/api/v1/document-versions/{version_id}/content",
        headers=headers,
        content=b"evidence",
    ).status_code == 200
    finalized = client.post(f"/api/v1/document-versions/{version_id}/finalize", headers=headers)
    assert finalized.status_code == 200, finalized.text
    downloaded = client.get(f"/api/v1/document-versions/{version_id}/download", headers=headers)
    assert downloaded.status_code == 200
    assert downloaded.content == b"evidence"
    stranger, stranger_headers = make_user("stranger@example.com")
    assert client.get(f"/api/v1/document-versions/{version_id}/download", headers=stranger_headers).status_code == 404
    other_workspace = workspace(stranger_headers, "Other metrics")
    foreign_metric = client.post(
        f"/api/v1/workspaces/{other_workspace}/metrics",
        headers=stranger_headers,
        json={"name": "Foreign"},
    ).json()
    foreign_version = client.post(
        f"/api/v1/metrics/{foreign_metric['id']}/versions",
        headers=stranger_headers,
        json={"formula": "gross_margin", "state": "accepted"},
    ).json()
    denied = client.post(
        f"/api/v1/projects/{created['id']}/dashboards",
        headers=headers,
        json={
            "title": "Borrowed",
            "definition": {
                "schema_version": 1,
                "widgets": [{"type": "metric_number", "metric_version_id": foreign_version["id"]}],
            },
        },
    )
    assert denied.status_code == 422
    assert denied.json()["code"] == "inaccessible_source"
    from app.jobs.worker import dispatch_outbox

    assert dispatch_outbox() >= 0
    assert client.get(f"/api/v1/projects/{created['id']}", headers=headers).status_code == 200
