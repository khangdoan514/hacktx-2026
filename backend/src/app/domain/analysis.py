import csv
import io
from decimal import Decimal

from psycopg.types.json import Json

from app.core.errors import AppError
from app.core.serialize import dump
from app.domain.calc import capacity_margin
from app.domain.common import emit, ensure_open, limit_of, one, require_project, require_workspace

REQUIRED_COLUMNS = (
    "partner_id",
    "contract_id",
    "contract_status",
    "contract_end",
    "period",
    "resource",
    "available_capacity",
    "committed_capacity",
    "consumed_capacity",
    "gpu_hours",
    "revenue",
    "included_cost",
    "currency",
    "extra_allocation",
)
ALLOWED_WIDGETS = {"metric_number", "capacity_table", "margin_table"}
UNSAFE_TOKENS = ("<script", "javascript:", "eval(", "text/html")


def create_dataset(conn, actor, workspace_id: str, name: str, description: str) -> dict:
    require_workspace(conn, workspace_id, {"owner", "administrator", "member"})
    row = conn.execute(
        """
        INSERT INTO app.datasets (workspace_id, name, description, owner_id)
        VALUES (%s, %s, %s, %s)
        RETURNING *
        """,
        (workspace_id, name.strip(), description or "", actor.user_id),
    ).fetchone()
    emit(
        conn,
        actor_id=actor.user_id,
        action="dataset.created",
        workspace_id=workspace_id,
        project_id=None,
        resource_type="dataset",
        resource_id=row["id"],
        revision=None,
        recipients=[],
        preview=row["name"],
    )
    return dump(dict(row))


def list_datasets(conn, workspace_id: str) -> dict:
    require_workspace(conn, workspace_id, {"owner", "administrator", "member"})
    rows = conn.execute(
        "SELECT * FROM app.datasets WHERE workspace_id = %s ORDER BY name",
        (workspace_id,),
    ).fetchall()
    return {"items": dump([dict(row) for row in rows])}


def import_csv(conn, actor, dataset_id: str, text: str) -> dict:
    dataset = one(conn, "SELECT * FROM app.datasets WHERE id = %s", (dataset_id,), "Dataset not found")
    require_workspace(conn, str(dataset["workspace_id"]), {"owner", "administrator", "member"})
    reader = csv.DictReader(io.StringIO(text))
    if not reader.fieldnames or any(column not in reader.fieldnames for column in REQUIRED_COLUMNS):
        raise AppError(422, "validation", "CSV is missing required columns", {"columns": list(REQUIRED_COLUMNS)})
    parsed: list[dict] = []
    seen: set[tuple] = set()
    quarantined = 0
    for index, raw in enumerate(reader, start=1):
        values = {column: (raw.get(column) or "").strip() for column in REQUIRED_COLUMNS}
        reason = ""
        key = (values["partner_id"], values["contract_id"], values["period"], values["resource"])
        if not values["partner_id"] or not values["contract_id"]:
            reason = "missing_identity"
        elif key in seen:
            reason = "duplicate_key"
        else:
            seen.add(key)
            if values["included_cost"] == "":
                reason = "missing_cost"
            else:
                try:
                    Decimal(values["revenue"] or "0")
                    Decimal(values["included_cost"])
                    Decimal(values["available_capacity"] or "0")
                    Decimal(values["committed_capacity"] or "0")
                    Decimal(values["extra_allocation"] or "0")
                except Exception:
                    reason = "malformed_number"
        if reason:
            quarantined += 1
        parsed.append({"row_number": index, "values": values, "quarantined": bool(reason), "reason": reason})
    if not parsed:
        raise AppError(422, "validation", "CSV has no data rows")
    state = "rejected" if quarantined == len(parsed) else "validated"
    latest = conn.execute(
        "SELECT COALESCE(MAX(revision), 0) AS revision FROM app.dataset_versions WHERE dataset_id = %s",
        (dataset_id,),
    ).fetchone()
    revision_no = latest["revision"] + 1
    summary = {
        "row_count": len(parsed),
        "quarantined": quarantined,
        "accepted": len(parsed) - quarantined,
        "policy": "quarantine_invalid_rows",
    }
    version = conn.execute(
        """
        INSERT INTO app.dataset_versions (
            dataset_id, workspace_id, revision, state, summary, actor_user_id
        ) VALUES (%s, %s, %s, %s, %s, %s)
        RETURNING *
        """,
        (dataset_id, dataset["workspace_id"], revision_no, state, Json(summary), actor.user_id),
    ).fetchone()
    for item in parsed:
        conn.execute(
            """
            INSERT INTO app.dataset_rows (
                version_id, workspace_id, row_number, partner_ref, values, quarantined, quarantine_reason
            ) VALUES (%s, %s, %s, %s, %s, %s, %s)
            """,
            (
                version["id"],
                dataset["workspace_id"],
                item["row_number"],
                item["values"]["partner_id"],
                Json(item["values"]),
                item["quarantined"],
                item["reason"],
            ),
        )
    emit(
        conn,
        actor_id=actor.user_id,
        action="dataset.imported",
        workspace_id=dataset["workspace_id"],
        project_id=None,
        resource_type="dataset_version",
        resource_id=version["id"],
        revision=revision_no,
        recipients=[],
        preview=f"Imported {summary['accepted']} rows",
    )
    payload = dump(dict(version))
    payload["summary"] = summary
    return payload


def create_metric(conn, actor, workspace_id: str, name: str) -> dict:
    require_workspace(conn, workspace_id, {"owner", "administrator", "member"})
    row = conn.execute(
        """
        INSERT INTO app.metric_definitions (workspace_id, name, owner_id)
        VALUES (%s, %s, %s)
        RETURNING *
        """,
        (workspace_id, name.strip(), actor.user_id),
    ).fetchone()
    return dump(dict(row))


def add_metric_version(conn, actor, metric_id: str, body: dict) -> dict:
    metric = one(conn, "SELECT * FROM app.metric_definitions WHERE id = %s", (metric_id,), "Metric not found")
    require_workspace(conn, str(metric["workspace_id"]), {"owner", "administrator", "member"})
    if body.get("formula", "gross_margin") != "gross_margin":
        raise AppError(422, "validation", "Only the registered gross_margin formula is available")
    latest = conn.execute(
        "SELECT COALESCE(MAX(revision), 0) AS revision FROM app.metric_versions WHERE metric_id = %s",
        (metric_id,),
    ).fetchone()
    row = conn.execute(
        """
        INSERT INTO app.metric_versions (
            metric_id, workspace_id, revision, formula, numerator, denominator,
            unit, period, exclusions, state, actor_user_id
        ) VALUES (%s, %s, %s, 'gross_margin', %s, %s, %s, %s, %s, %s, %s)
        RETURNING *
        """,
        (
            metric_id,
            metric["workspace_id"],
            latest["revision"] + 1,
            body.get("numerator") or "revenue - included_cost",
            body.get("denominator") or "revenue",
            body.get("unit") or "ratio",
            body.get("period") or "",
            Json(body.get("exclusions") or []),
            body.get("state") or "draft",
            actor.user_id,
        ),
    ).fetchone()
    return dump(dict(row))


def accept_metric_version(conn, actor, version_id: str) -> dict:
    version = one(conn, "SELECT * FROM app.metric_versions WHERE id = %s", (version_id,), "Metric version not found")
    require_workspace(conn, str(version["workspace_id"]), {"owner", "administrator", "member"})
    row = conn.execute(
        """
        UPDATE app.metric_versions
        SET state = 'accepted'
        WHERE id = %s
        RETURNING *
        """,
        (version_id,),
    ).fetchone()
    _ = actor
    return dump(dict(row))


def ask_question(conn, actor, workspace_id: str, body: dict) -> dict:
    require_workspace(conn, workspace_id, {"owner", "administrator", "member"})
    if body.get("assignee_id"):
        member = conn.execute(
            """
            SELECT 1 FROM app.workspace_memberships
            WHERE workspace_id = %s AND user_id = %s AND status = 'active'
              AND access_role IN ('owner', 'administrator', 'member')
            """,
            (workspace_id, body["assignee_id"]),
        ).fetchone()
        if not member:
            raise AppError(422, "validation", "Assignee must be an internal workspace member")
    row = conn.execute(
        """
        INSERT INTO app.data_questions (
            workspace_id, dataset_id, metric_id, analysis_id, requester_id, assignee_id, question
        ) VALUES (%s, %s, %s, %s, %s, %s, %s)
        RETURNING *
        """,
        (
            workspace_id,
            body.get("dataset_id"),
            body.get("metric_id"),
            body.get("analysis_id"),
            actor.user_id,
            body.get("assignee_id"),
            body["question"].strip(),
        ),
    ).fetchone()
    emit(
        conn,
        actor_id=actor.user_id,
        action="question.asked",
        workspace_id=workspace_id,
        project_id=None,
        resource_type="data_question",
        resource_id=row["id"],
        revision=None,
        recipients=[body["assignee_id"]] if body.get("assignee_id") else [],
        preview=row["question"][:180],
    )
    return dump(dict(row))


def answer_question(conn, actor, question_id: str, text: str, accept: bool) -> dict:
    question = one(conn, "SELECT * FROM app.data_questions WHERE id = %s", (question_id,), "Question not found")
    require_workspace(conn, str(question["workspace_id"]), {"owner", "administrator", "member"})
    if question["state"] == "accepted":
        raise AppError(409, "invalid_state", "This question is already accepted")
    conn.execute(
        """
        INSERT INTO app.question_responses (question_id, workspace_id, body, actor_user_id, accepted)
        VALUES (%s, %s, %s, %s, %s)
        """,
        (question_id, question["workspace_id"], text.strip(), actor.user_id, accept),
    )
    state = "accepted" if accept else "answered"
    row = conn.execute(
        """
        UPDATE app.data_questions
        SET state = %s, updated_at = now()
        WHERE id = %s
        RETURNING *
        """,
        (state, question_id),
    ).fetchone()
    emit(
        conn,
        actor_id=actor.user_id,
        action=f"question.{state}",
        workspace_id=question["workspace_id"],
        project_id=None,
        resource_type="data_question",
        resource_id=question_id,
        revision=None,
        recipients=[str(question["requester_id"])],
        preview=text[:180],
    )
    return dump(dict(row))


def list_questions(conn, workspace_id: str) -> dict:
    require_workspace(conn, workspace_id, {"owner", "administrator", "member"})
    rows = conn.execute(
        "SELECT * FROM app.data_questions WHERE workspace_id = %s ORDER BY created_at",
        (workspace_id,),
    ).fetchall()
    return {"items": dump([dict(row) for row in rows])}


def list_analyses(conn, project_id: str, limit: int | None) -> dict:
    require_project(conn, project_id, {"owner", "contributor", "reviewer", "viewer"})
    rows = conn.execute(
        """
        SELECT * FROM app.analyses
        WHERE project_id = %s
        ORDER BY created_at, id
        LIMIT %s
        """,
        (project_id, limit_of(limit)),
    ).fetchall()
    return {"items": dump([dict(row) for row in rows])}


def create_analysis(conn, actor, project_id: str, title: str, question: str) -> dict:
    project = require_project(conn, project_id, {"owner", "contributor", "reviewer", "viewer"})
    ensure_open(project)
    row = conn.execute(
        """
        INSERT INTO app.analyses (workspace_id, project_id, title, question, created_by)
        VALUES (%s, %s, %s, %s, %s)
        RETURNING *
        """,
        (project["workspace_id"], project_id, title.strip(), question or "", actor.user_id),
    ).fetchone()
    return dump(dict(row))


def calculate_analysis(conn, actor, analysis_id: str, body: dict) -> dict:
    analysis = one(conn, "SELECT * FROM app.analyses WHERE id = %s", (analysis_id,), "Analysis not found")
    project = require_project(conn, str(analysis["project_id"]), {"owner", "contributor", "reviewer"})
    ensure_open(project)
    version = one(
        conn,
        """
        SELECT * FROM app.dataset_versions
        WHERE id = %s AND state = 'validated'
        """,
        (body["dataset_version_id"],),
        "Validated dataset version not found",
    )
    metric = one(
        conn,
        """
        SELECT * FROM app.metric_versions
        WHERE id = %s AND state = 'accepted' AND formula = 'gross_margin'
        """,
        (body["metric_version_id"],),
        "Accepted metric version not found",
    )
    rows = conn.execute(
        """
        SELECT partner_ref, values, quarantined, quarantine_reason
        FROM app.dataset_rows
        WHERE version_id = %s
        ORDER BY row_number
        """,
        (version["id"],),
    ).fetchall()
    parameters = {
        "target_margin": body["target_margin"],
        "forecast_period": body["forecast_period"],
        "exclude_contract_end_before": body.get("exclude_contract_end_before"),
    }
    output = capacity_margin([dict(row) for row in rows], parameters)
    quality = {
        "excluded": output["excluded"],
        "capacity_shortfall": output["capacity_shortfall"],
        "missing_cost_treated_as_zero": False,
    }
    latest = conn.execute(
        "SELECT COALESCE(MAX(revision), 0) AS revision FROM app.analysis_revisions WHERE analysis_id = %s",
        (analysis_id,),
    ).fetchone()
    row = conn.execute(
        """
        INSERT INTO app.analysis_revisions (
            analysis_id, workspace_id, project_id, revision, dataset_version_id, metric_version_id,
            parameters, output, quality, actor_user_id
        ) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
        RETURNING *
        """,
        (
            analysis_id,
            analysis["workspace_id"],
            analysis["project_id"],
            latest["revision"] + 1,
            version["id"],
            metric["id"],
            Json(parameters),
            Json(output),
            Json(quality),
            actor.user_id,
        ),
    ).fetchone()
    conn.execute(
        "UPDATE app.analyses SET updated_at = now() WHERE id = %s",
        (analysis_id,),
    )
    emit(
        conn,
        actor_id=actor.user_id,
        action="analysis.calculated",
        workspace_id=analysis["workspace_id"],
        project_id=analysis["project_id"],
        resource_type="analysis_revision",
        resource_id=row["id"],
        revision=row["revision"],
        recipients=[],
        preview=analysis["title"],
    )
    return dump(dict(row))


def get_analysis(conn, analysis_id: str) -> dict:
    analysis = one(conn, "SELECT * FROM app.analyses WHERE id = %s", (analysis_id,), "Analysis not found")
    revisions = conn.execute(
        "SELECT * FROM app.analysis_revisions WHERE analysis_id = %s ORDER BY revision",
        (analysis_id,),
    ).fetchall()
    payload = dump(dict(analysis))
    payload["revisions"] = dump([dict(row) for row in revisions])
    return payload


def _validate_definition(conn, project, definition: dict) -> None:
    if definition.get("schema_version") != 1:
        raise AppError(422, "invalid_definition", "Unsupported dashboard schema")
    blob = str(definition).lower()
    if any(token in blob for token in UNSAFE_TOKENS):
        raise AppError(422, "unsafe_definition", "Dashboard definitions cannot contain executable content")
    widgets = definition.get("widgets")
    if not isinstance(widgets, list) or not widgets:
        raise AppError(422, "invalid_definition", "Add at least one widget")
    for widget in widgets:
        if not isinstance(widget, dict) or widget.get("type") not in ALLOWED_WIDGETS:
            raise AppError(422, "unsupported_widget", "Widget type is not in the catalog")
        for banned in ("sql", "script", "html", "code"):
            if banned in widget:
                raise AppError(422, "unsafe_definition", "Widget configuration is not allowed")
        metric_id = widget.get("metric_version_id")
        if widget["type"] == "metric_number":
            if not metric_id:
                raise AppError(422, "invalid_definition", "metric_number needs a metric version")
            found = conn.execute(
                """
                SELECT id FROM app.metric_versions
                WHERE id = %s AND state = 'accepted' AND workspace_id = %s
                """,
                (metric_id, project["workspace_id"]),
            ).fetchone()
            if not found:
                raise AppError(422, "invalid_definition", "Metric version is not an accepted version you can use")


def create_dashboard(conn, actor, project_id: str, title: str, definition: dict) -> dict:
    project = require_project(conn, project_id, {"owner", "contributor"})
    ensure_open(project)
    _validate_definition(conn, project, definition)
    dashboard = conn.execute(
        """
        INSERT INTO app.dashboards (workspace_id, project_id, title, created_by)
        VALUES (%s, %s, %s, %s)
        RETURNING *
        """,
        (project["workspace_id"], project_id, title.strip(), actor.user_id),
    ).fetchone()
    revision = conn.execute(
        """
        INSERT INTO app.dashboard_revisions (
            dashboard_id, workspace_id, project_id, revision, definition, state, actor_user_id
        ) VALUES (%s, %s, %s, 1, %s, 'draft', %s)
        RETURNING *
        """,
        (dashboard["id"], project["workspace_id"], project_id, Json(definition), actor.user_id),
    ).fetchone()
    payload = dump(dict(dashboard))
    payload["revision"] = dump(dict(revision))
    return payload


def add_dashboard_revision(conn, actor, dashboard_id: str, definition: dict, base_revision: int | None) -> dict:
    dashboard = one(conn, "SELECT * FROM app.dashboards WHERE id = %s", (dashboard_id,), "Dashboard not found")
    project = require_project(conn, str(dashboard["project_id"]), {"owner", "contributor"})
    ensure_open(project)
    _validate_definition(conn, project, definition)
    latest = conn.execute(
        "SELECT MAX(revision) AS revision FROM app.dashboard_revisions WHERE dashboard_id = %s",
        (dashboard_id,),
    ).fetchone()
    if base_revision is not None and int(base_revision) != int(latest["revision"]):
        raise AppError(
            409,
            "conflict",
            "The dashboard changed. Reload and edit the latest revision.",
            {"revision": latest["revision"]},
        )
    row = conn.execute(
        """
        INSERT INTO app.dashboard_revisions (
            dashboard_id, workspace_id, project_id, revision, base_revision, definition, state, actor_user_id
        ) VALUES (%s, %s, %s, %s, %s, %s, 'draft', %s)
        RETURNING *
        """,
        (
            dashboard_id,
            dashboard["workspace_id"],
            dashboard["project_id"],
            latest["revision"] + 1,
            base_revision,
            Json(definition),
            actor.user_id,
        ),
    ).fetchone()
    return dump(dict(row))


def publish_dashboard(conn, actor, revision_id: str) -> dict:
    revision = one(
        conn,
        "SELECT * FROM app.dashboard_revisions WHERE id = %s",
        (revision_id,),
        "Dashboard revision not found",
    )
    project = require_project(conn, str(revision["project_id"]), {"owner", "contributor"})
    ensure_open(project)
    if revision["state"] == "published":
        return dump(dict(revision))
    _validate_definition(conn, project, revision["definition"])
    row = conn.execute(
        """
        UPDATE app.dashboard_revisions
        SET state = 'published'
        WHERE id = %s AND state = 'draft'
        RETURNING *
        """,
        (revision_id,),
    ).fetchone()
    emit(
        conn,
        actor_id=actor.user_id,
        action="dashboard.published",
        workspace_id=revision["workspace_id"],
        project_id=revision["project_id"],
        resource_type="dashboard_revision",
        resource_id=revision_id,
        revision=revision["revision"],
        recipients=[],
        preview="Dashboard revision published",
    )
    return dump(dict(row))


def get_dashboard(conn, dashboard_id: str) -> dict:
    dashboard = one(conn, "SELECT * FROM app.dashboards WHERE id = %s", (dashboard_id,), "Dashboard not found")
    revisions = conn.execute(
        "SELECT * FROM app.dashboard_revisions WHERE dashboard_id = %s ORDER BY revision",
        (dashboard_id,),
    ).fetchall()
    payload = dump(dict(dashboard))
    payload["revisions"] = dump([dict(row) for row in revisions])
    return payload


def list_dashboards(conn, project_id: str, limit: int | None) -> dict:
    require_project(conn, project_id, {"owner", "contributor", "reviewer", "viewer"})
    rows = conn.execute(
        """
        SELECT * FROM app.dashboards
        WHERE project_id = %s
        ORDER BY created_at, id
        LIMIT %s
        """,
        (project_id, limit_of(limit)),
    ).fetchall()
    return {"items": dump([dict(row) for row in rows])}
