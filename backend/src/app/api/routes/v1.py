from fastapi import APIRouter, Depends, Header, Query, Request, Response
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field

from app.core.auth import Actor, LocalAuthIn, get_actor, local_session
from app.core.database import user_connection
from app.domain import analysis, files, tenancy, work
from app.jobs.worker import dispatch_outbox

router = APIRouter(prefix="/v1")


class WorkspaceIn(BaseModel):
    name: str = Field(min_length=1, max_length=200)
    timezone: str = "UTC"


class WorkspacePatch(BaseModel):
    expected_version: int
    name: str | None = None
    timezone: str | None = None


class MemberPatch(BaseModel):
    access_role: str | None = None
    status: str | None = None


class DepartmentIn(BaseModel):
    name: str


class FunctionIn(BaseModel):
    user_id: str
    code: str


class InvitationIn(BaseModel):
    email: str
    access_role: str = "guest"
    project_id: str | None = None
    project_role: str | None = None
    partner_id: str | None = None


class ProjectIn(BaseModel):
    name: str
    project_type: str
    objective: str = ""
    constraints: str = ""
    scope: str = ""
    partner_id: str | None = None
    parent_id: str | None = None
    starts_on: str | None = None
    ends_on: str | None = None
    internal_notes: str | None = None
    internal_cost: str | None = None


class ProjectPatch(BaseModel):
    expected_version: int
    name: str | None = None
    objective: str | None = None
    constraints: str | None = None
    scope: str | None = None
    starts_on: str | None = None
    ends_on: str | None = None
    internal_notes: str | None = None
    internal_cost: str | None = None


class ProjectMemberIn(BaseModel):
    user_id: str
    access_role: str


class PartnerIn(BaseModel):
    name: str


class ContactIn(BaseModel):
    name: str
    email: str


class ProfilePatch(BaseModel):
    display_name: str | None = None
    timezone: str | None = None


class RequestIn(BaseModel):
    title: str
    outcome: str = ""
    constraints: str = ""
    due_on: str | None = None
    project_id: str | None = None


class TransitionIn(BaseModel):
    expected_version: int
    reason: str = ""
    evidence: str = ""


class RequirementIn(BaseModel):
    title: str
    body: str = ""
    acceptance_criteria: str = ""
    priority: str = "medium"
    request_id: str | None = None


class RequirementRevisionIn(BaseModel):
    expected_version: int
    body: str = ""
    acceptance_criteria: str = ""
    priority: str = "medium"


class TaskIn(BaseModel):
    title: str
    brief: str = ""
    acceptance_criteria: str = ""
    assignee_id: str | None = None
    priority: int = 3
    due_on: str | None = None
    parent_id: str | None = None
    requirement_id: str | None = None


class TaskPatch(BaseModel):
    expected_version: int
    title: str | None = None
    brief: str | None = None
    acceptance_criteria: str | None = None
    assignee_id: str | None = None
    priority: int | None = None
    due_on: str | None = None
    blocked_reason: str | None = None


class DependencyIn(BaseModel):
    depends_on: str


class HandoffSend(BaseModel):
    expected_version: int
    objective: str
    requested_output: str
    recipient_id: str
    accountable_id: str | None = None
    due_on: str | None = None
    assumptions: str = ""
    constraints: str = ""
    findings: str = ""
    open_questions: list[str] = []
    source_references: list[dict] = []


class HandoffMove(BaseModel):
    expected_version: int
    note: str = ""
    recipient_id: str | None = None


class ApprovalIn(BaseModel):
    target_type: str
    target_id: str
    target_revision: int
    reviewer_id: str


class DecisionIn(BaseModel):
    expected_version: int
    target_revision: int
    comment: str = ""


class CommentIn(BaseModel):
    target_type: str
    target_id: str
    body: str
    visibility: str = "internal"
    mentions: list[str] = []


class DocumentIn(BaseModel):
    title: str
    filename: str
    mime_type: str
    visibility: str = "internal"


class DatasetIn(BaseModel):
    name: str
    description: str = ""


class CsvIn(BaseModel):
    csv: str


class MetricIn(BaseModel):
    name: str


class MetricVersionIn(BaseModel):
    formula: str = "gross_margin"
    period: str = ""
    numerator: str = "revenue - included_cost"
    denominator: str = "revenue"
    unit: str = "ratio"
    exclusions: list[str] = []
    state: str = "draft"


class QuestionIn(BaseModel):
    question: str
    dataset_id: str | None = None
    metric_id: str | None = None
    analysis_id: str | None = None
    assignee_id: str | None = None


class AnswerIn(BaseModel):
    body: str
    accept: bool = False


class AnalysisIn(BaseModel):
    title: str
    question: str = ""


class CalculateIn(BaseModel):
    dataset_version_id: str
    metric_version_id: str
    target_margin: str
    forecast_period: str
    exclude_contract_end_before: str | None = None


class DashboardIn(BaseModel):
    title: str
    definition: dict


class DashboardRevisionIn(BaseModel):
    definition: dict
    base_revision: int | None = None


def _finish(result):
    if isinstance(result, dict) and {"replay", "status", "body"} <= set(result):
        return JSONResponse(status_code=result["status"], content=result["body"])
    return result


def _write(actor: Actor, fn, *args):
    with user_connection(actor.user_id, actor.email) as conn:
        result = fn(conn, *args)
    dispatch_outbox()
    return _finish(result)


def _read(actor: Actor, fn, *args):
    with user_connection(actor.user_id, actor.email) as conn:
        return fn(conn, *args)


@router.post("/auth/local-session")
def post_local_session(body: LocalAuthIn):
    return local_session(body)


@router.get("/me")
def get_me(actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: tenancy.get_me(conn, actor))


@router.patch("/me")
def patch_me(body: ProfilePatch, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: tenancy.update_me(conn, actor, body.display_name, body.timezone))


@router.get("/workspaces")
def get_workspaces(limit: int | None = None, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: tenancy.list_workspaces(conn, limit))


@router.post("/workspaces", status_code=201)
def post_workspace(body: WorkspaceIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: tenancy.create_workspace(conn, actor, body.name, body.timezone))


@router.get("/workspaces/{workspace_id}")
def get_workspace(workspace_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: tenancy.get_workspace(conn, workspace_id))


@router.patch("/workspaces/{workspace_id}")
def patch_workspace(workspace_id: str, body: WorkspacePatch, actor: Actor = Depends(get_actor)):
    fields = body.model_dump()
    return _write(actor, lambda conn: tenancy.update_workspace(conn, actor, workspace_id, body.expected_version, fields))


@router.get("/workspaces/{workspace_id}/members")
def get_members(workspace_id: str, limit: int | None = None, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: tenancy.list_members(conn, workspace_id, limit))


@router.patch("/workspaces/{workspace_id}/members/{user_id}")
def patch_member(workspace_id: str, user_id: str, body: MemberPatch, actor: Actor = Depends(get_actor)):
    return _write(
        actor,
        lambda conn: tenancy.update_member(conn, actor, workspace_id, user_id, body.access_role, body.status),
    )


@router.get("/workspaces/{workspace_id}/departments")
def get_departments(workspace_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: tenancy.list_departments(conn, workspace_id))


@router.post("/workspaces/{workspace_id}/departments", status_code=201)
def post_department(workspace_id: str, body: DepartmentIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: tenancy.add_department(conn, actor, workspace_id, body.name))


@router.post("/workspaces/{workspace_id}/functions", status_code=201)
def post_function(workspace_id: str, body: FunctionIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: tenancy.assign_function(conn, actor, workspace_id, body.user_id, body.code))


@router.post("/workspaces/{workspace_id}/invitations", status_code=201)
def post_invitation(workspace_id: str, body: InvitationIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: tenancy.create_invitation(conn, actor, workspace_id, body.model_dump()))


@router.post("/workspaces/{workspace_id}/invitations/{invitation_id}/revoke")
def post_revoke(workspace_id: str, invitation_id: str, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: tenancy.revoke_invitation(conn, actor, workspace_id, invitation_id))


@router.get("/invitations/{token}")
def get_invitation(token: str):
    return tenancy.preview_invitation(token)


@router.post("/invitations/{token}/accept")
def post_accept(token: str, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: tenancy.accept_invitation(conn, actor, token))


@router.get("/workspaces/{workspace_id}/projects")
def get_projects(workspace_id: str, limit: int | None = None, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: tenancy.list_projects(conn, workspace_id, limit, False))


@router.get("/workspaces/{workspace_id}/partner/projects")
def get_partner_projects(workspace_id: str, limit: int | None = None, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: tenancy.list_projects(conn, workspace_id, limit, True))


@router.post("/workspaces/{workspace_id}/projects", status_code=201)
def post_project(workspace_id: str, body: ProjectIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: tenancy.create_project(conn, actor, workspace_id, body.model_dump()))


@router.get("/projects/{project_id}")
def get_project(project_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: tenancy.get_project(conn, project_id))


@router.patch("/projects/{project_id}")
def patch_project(project_id: str, body: ProjectPatch, actor: Actor = Depends(get_actor)):
    return _write(
        actor,
        lambda conn: tenancy.update_project(conn, actor, project_id, body.expected_version, body.model_dump()),
    )


@router.post("/projects/{project_id}/archive")
def post_archive(project_id: str, body: TransitionIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: tenancy.set_project_state(conn, actor, project_id, body.expected_version, "archived"))


@router.post("/projects/{project_id}/restore")
def post_restore(project_id: str, body: TransitionIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: tenancy.set_project_state(conn, actor, project_id, body.expected_version, "active"))


@router.get("/projects/{project_id}/members")
def get_project_members(project_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: tenancy.list_project_members(conn, project_id))


@router.post("/projects/{project_id}/members", status_code=201)
def post_project_member(project_id: str, body: ProjectMemberIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: tenancy.add_project_member(conn, actor, project_id, body.user_id, body.access_role))


@router.get("/workspaces/{workspace_id}/partners")
def get_partners(workspace_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: tenancy.list_partners(conn, workspace_id))


@router.post("/workspaces/{workspace_id}/partners", status_code=201)
def post_partner(workspace_id: str, body: PartnerIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: tenancy.create_partner(conn, actor, workspace_id, body.name))


@router.post("/partners/{partner_id}/contacts", status_code=201)
def post_contact(partner_id: str, body: ContactIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: tenancy.add_partner_contact(conn, actor, partner_id, body.name, body.email))


@router.post("/partners/{partner_id}/suspend")
def post_suspend(partner_id: str, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: tenancy.suspend_partner(conn, actor, partner_id))


@router.get("/workspaces/{workspace_id}/requests")
def get_requests(workspace_id: str, limit: int | None = None, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: work.list_requests(conn, workspace_id, limit))


@router.post("/workspaces/{workspace_id}/requests", status_code=201)
def post_request(workspace_id: str, body: RequestIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: work.create_request(conn, actor, workspace_id, body.model_dump()))


@router.get("/requests/{request_id}")
def get_request(request_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: work.get_request(conn, request_id))


def _request_move(request_id: str, target: str, body: TransitionIn, actor: Actor):
    return _write(actor, lambda conn: work.transition_request(conn, actor, request_id, body.expected_version, target, body.reason))


@router.post("/requests/{request_id}/submit")
def post_submit_request(request_id: str, body: TransitionIn, actor: Actor = Depends(get_actor)):
    return _request_move(request_id, "submitted", body, actor)


@router.post("/requests/{request_id}/triage")
def post_triage(request_id: str, body: TransitionIn, actor: Actor = Depends(get_actor)):
    return _request_move(request_id, "triaging", body, actor)


@router.post("/requests/{request_id}/request-information")
def post_request_info(request_id: str, body: TransitionIn, actor: Actor = Depends(get_actor)):
    return _request_move(request_id, "needs_information", body, actor)


@router.post("/requests/{request_id}/accept")
def post_accept_request(request_id: str, body: TransitionIn, actor: Actor = Depends(get_actor)):
    return _request_move(request_id, "accepted", body, actor)


@router.post("/requests/{request_id}/reject")
def post_reject_request(request_id: str, body: TransitionIn, actor: Actor = Depends(get_actor)):
    return _request_move(request_id, "rejected", body, actor)


@router.post("/requests/{request_id}/convert")
def post_convert(
    request_id: str,
    actor: Actor = Depends(get_actor),
    idempotency_key: str | None = Header(default=None, alias="Idempotency-Key"),
):
    return _write(actor, lambda conn: work.convert_request(conn, actor, request_id, idempotency_key))


@router.get("/projects/{project_id}/requirements")
def get_requirements(project_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: work.list_requirements(conn, project_id))


@router.post("/projects/{project_id}/requirements", status_code=201)
def post_requirement(project_id: str, body: RequirementIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: work.create_requirement(conn, actor, project_id, body.model_dump()))


@router.get("/requirements/{requirement_id}")
def get_requirement(requirement_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: work.get_requirement(conn, requirement_id))


@router.post("/requirements/{requirement_id}/revisions", status_code=201)
def post_requirement_revision(requirement_id: str, body: RequirementRevisionIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: work.add_requirement_revision(conn, actor, requirement_id, body.model_dump()))


@router.post("/requirements/{requirement_id}/submit-review")
def post_requirement_review(requirement_id: str, body: TransitionIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: work.transition_requirement(conn, actor, requirement_id, body.expected_version, "in_review"))


@router.post("/requirements/{requirement_id}/accept")
def post_requirement_accept(requirement_id: str, body: TransitionIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: work.transition_requirement(conn, actor, requirement_id, body.expected_version, "accepted"))


@router.post("/requirements/{requirement_id}/request-changes")
def post_requirement_changes(requirement_id: str, body: TransitionIn, actor: Actor = Depends(get_actor)):
    return _write(
        actor,
        lambda conn: work.transition_requirement(conn, actor, requirement_id, body.expected_version, "changes_requested"),
    )


@router.get("/projects/{project_id}/tasks")
def get_tasks(project_id: str, limit: int | None = None, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: work.list_tasks(conn, project_id, limit))


@router.post("/projects/{project_id}/tasks", status_code=201)
def post_task(project_id: str, body: TaskIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: work.create_task(conn, actor, project_id, body.model_dump()))


@router.get("/tasks/{task_id}")
def get_task(task_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: work.get_task(conn, task_id))


@router.patch("/tasks/{task_id}")
def patch_task(task_id: str, body: TaskPatch, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: work.update_task(conn, actor, task_id, body.expected_version, body.model_dump()))


def _task_move(task_id: str, target: str, body: TransitionIn, actor: Actor):
    return _write(
        actor,
        lambda conn: work.transition_task(conn, actor, task_id, body.expected_version, target, body.reason, body.evidence),
    )


@router.post("/tasks/{task_id}/start")
def post_start_task(task_id: str, body: TransitionIn, actor: Actor = Depends(get_actor)):
    return _task_move(task_id, "in_progress", body, actor)


@router.post("/tasks/{task_id}/submit-review")
def post_task_review(task_id: str, body: TransitionIn, actor: Actor = Depends(get_actor)):
    return _task_move(task_id, "in_review", body, actor)


@router.post("/tasks/{task_id}/complete")
def post_complete_task(task_id: str, body: TransitionIn, actor: Actor = Depends(get_actor)):
    return _task_move(task_id, "done", body, actor)


@router.post("/tasks/{task_id}/request-changes")
def post_task_changes(task_id: str, body: TransitionIn, actor: Actor = Depends(get_actor)):
    return _task_move(task_id, "changes_requested", body, actor)


@router.post("/tasks/{task_id}/reopen")
def post_reopen_task(task_id: str, body: TransitionIn, actor: Actor = Depends(get_actor)):
    return _task_move(task_id, "in_progress", body, actor)


@router.post("/tasks/{task_id}/cancel")
def post_cancel_task(task_id: str, body: TransitionIn, actor: Actor = Depends(get_actor)):
    return _task_move(task_id, "cancelled", body, actor)


@router.post("/tasks/{task_id}/dependencies", status_code=201)
def post_dependency(task_id: str, body: DependencyIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: work.add_dependency(conn, actor, task_id, body.depends_on))


@router.get("/workspaces/{workspace_id}/my-work")
def get_my_work(workspace_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: work.my_work(conn, workspace_id))


@router.get("/projects/{project_id}/handoffs")
def get_handoffs(project_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: work.list_handoffs(conn, project_id))


@router.post("/projects/{project_id}/handoffs", status_code=201)
def post_handoff(project_id: str, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: work.create_handoff(conn, actor, project_id, {}))


@router.get("/handoffs/{handoff_id}")
def get_handoff(handoff_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: work.get_handoff(conn, handoff_id))


@router.post("/handoffs/{handoff_id}/send")
def post_send_handoff(handoff_id: str, body: HandoffSend, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: work.send_handoff(conn, actor, handoff_id, body.expected_version, body.model_dump()))


@router.post("/handoffs/{handoff_id}/acknowledge")
def post_ack(handoff_id: str, body: HandoffMove, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: work.transition_handoff(conn, actor, handoff_id, body.expected_version, "acknowledged", body.note))


@router.post("/handoffs/{handoff_id}/return")
def post_return(handoff_id: str, body: HandoffMove, actor: Actor = Depends(get_actor)):
    return _write(
        actor,
        lambda conn: work.transition_handoff(conn, actor, handoff_id, body.expected_version, "needs_information", body.note),
    )


@router.post("/handoffs/{handoff_id}/reassign")
def post_reassign(handoff_id: str, body: HandoffMove, actor: Actor = Depends(get_actor)):
    return _write(
        actor,
        lambda conn: work.transition_handoff(
            conn, actor, handoff_id, body.expected_version, "sent", body.note, body.recipient_id
        ),
    )


@router.post("/handoffs/{handoff_id}/complete")
def post_complete_handoff(handoff_id: str, body: HandoffMove, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: work.transition_handoff(conn, actor, handoff_id, body.expected_version, "completed", body.note))


@router.post("/handoffs/{handoff_id}/cancel")
def post_cancel_handoff(handoff_id: str, body: HandoffMove, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: work.transition_handoff(conn, actor, handoff_id, body.expected_version, "cancelled", body.note))


@router.get("/workspaces/{workspace_id}/approvals")
def get_approvals(workspace_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: work.list_approvals(conn, workspace_id))


@router.post("/projects/{project_id}/approvals", status_code=201)
def post_approval(project_id: str, body: ApprovalIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: work.create_approval(conn, actor, project_id, body.model_dump()))


@router.get("/approvals/{approval_id}")
def get_approval(approval_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: work.get_approval(conn, approval_id))


def _decide(approval_id: str, decision: str, body: DecisionIn, actor: Actor, key: str | None):
    return _write(
        actor,
        lambda conn: work.decide_approval(
            conn, actor, approval_id, body.expected_version, decision, body.comment, key, body.target_revision
        ),
    )


@router.post("/approvals/{approval_id}/approve")
def post_approve(
    approval_id: str,
    body: DecisionIn,
    actor: Actor = Depends(get_actor),
    idempotency_key: str | None = Header(default=None, alias="Idempotency-Key"),
):
    return _decide(approval_id, "approved", body, actor, idempotency_key)


@router.post("/approvals/{approval_id}/reject")
def post_reject(
    approval_id: str,
    body: DecisionIn,
    actor: Actor = Depends(get_actor),
    idempotency_key: str | None = Header(default=None, alias="Idempotency-Key"),
):
    return _decide(approval_id, "rejected", body, actor, idempotency_key)


@router.post("/approvals/{approval_id}/request-changes")
def post_approval_changes(
    approval_id: str,
    body: DecisionIn,
    actor: Actor = Depends(get_actor),
    idempotency_key: str | None = Header(default=None, alias="Idempotency-Key"),
):
    return _decide(approval_id, "changes_requested", body, actor, idempotency_key)


@router.get("/projects/{project_id}/documents")
def get_documents(project_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: files.list_documents(conn, project_id))


@router.post("/projects/{project_id}/documents", status_code=201)
def post_document(project_id: str, body: DocumentIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: files.initiate(conn, actor, project_id, body.model_dump()))


@router.post("/document-versions/{version_id}/content")
async def post_content(version_id: str, request: Request, actor: Actor = Depends(get_actor)):
    data = await request.body()
    return _write(actor, lambda conn: files.store_bytes(conn, actor, version_id, data))


@router.post("/document-versions/{version_id}/finalize")
def post_finalize(version_id: str, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: files.finalize(conn, actor, version_id))


@router.get("/document-versions/{version_id}/download")
def get_download(version_id: str, actor: Actor = Depends(get_actor)):
    with user_connection(actor.user_id, actor.email) as conn:
        meta, data = files.open_bytes(conn, version_id)
    return Response(content=data, media_type=meta["mime_type"], headers={"X-Checksum": meta["checksum"]})


@router.get("/projects/{project_id}/comments")
def get_comments(
    project_id: str,
    target_type: str | None = None,
    target_id: str | None = None,
    actor: Actor = Depends(get_actor),
):
    return _read(actor, lambda conn: work.list_comments(conn, project_id, target_type, target_id))


@router.post("/projects/{project_id}/comments", status_code=201)
def post_comment(project_id: str, body: CommentIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: work.create_comment(conn, actor, project_id, body.model_dump()))


@router.get("/workspaces/{workspace_id}/notifications")
def get_notifications(workspace_id: str, limit: int | None = None, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: work.list_notifications(conn, workspace_id, limit))


@router.post("/notifications/{notification_id}/read")
def post_read(notification_id: str, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: work.mark_notification_read(conn, notification_id))


@router.post("/workspaces/{workspace_id}/notifications/read-all")
def post_read_all(workspace_id: str, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: work.mark_all_read(conn, workspace_id))


@router.get("/workspaces/{workspace_id}/audit")
def get_audit(workspace_id: str, limit: int | None = None, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: work.list_audit(conn, workspace_id, None, limit))


@router.get("/projects/{project_id}/activity")
def get_activity(project_id: str, workspace_id: str = Query(), limit: int | None = None, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: work.list_audit(conn, workspace_id, project_id, limit))


@router.get("/workspaces/{workspace_id}/datasets")
def get_datasets(workspace_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: analysis.list_datasets(conn, workspace_id))


@router.post("/workspaces/{workspace_id}/datasets", status_code=201)
def post_dataset(workspace_id: str, body: DatasetIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: analysis.create_dataset(conn, actor, workspace_id, body.name, body.description))


@router.post("/datasets/{dataset_id}/imports", status_code=201)
def post_import(dataset_id: str, body: CsvIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: analysis.import_csv(conn, actor, dataset_id, body.csv))


@router.post("/workspaces/{workspace_id}/metrics", status_code=201)
def post_metric(workspace_id: str, body: MetricIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: analysis.create_metric(conn, actor, workspace_id, body.name))


@router.post("/metrics/{metric_id}/versions", status_code=201)
def post_metric_version(metric_id: str, body: MetricVersionIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: analysis.add_metric_version(conn, actor, metric_id, body.model_dump()))


@router.post("/metric-versions/{version_id}/accept")
def post_accept_metric(version_id: str, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: analysis.accept_metric_version(conn, actor, version_id))


@router.get("/workspaces/{workspace_id}/questions")
def get_questions(workspace_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: analysis.list_questions(conn, workspace_id))


@router.post("/workspaces/{workspace_id}/questions", status_code=201)
def post_question(workspace_id: str, body: QuestionIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: analysis.ask_question(conn, actor, workspace_id, body.model_dump()))


@router.post("/questions/{question_id}/responses", status_code=201)
def post_answer(question_id: str, body: AnswerIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: analysis.answer_question(conn, actor, question_id, body.body, body.accept))


@router.get("/projects/{project_id}/analyses")
def get_analyses(project_id: str, limit: int | None = None, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: analysis.list_analyses(conn, project_id, limit))


@router.post("/projects/{project_id}/analyses", status_code=201)
def post_analysis(project_id: str, body: AnalysisIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: analysis.create_analysis(conn, actor, project_id, body.title, body.question))


@router.get("/analyses/{analysis_id}")
def get_analysis(analysis_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: analysis.get_analysis(conn, analysis_id))


@router.post("/analyses/{analysis_id}/calculate")
def post_calculate(analysis_id: str, body: CalculateIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: analysis.calculate_analysis(conn, actor, analysis_id, body.model_dump()))


@router.get("/projects/{project_id}/dashboards")
def get_dashboards(project_id: str, limit: int | None = None, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: analysis.list_dashboards(conn, project_id, limit))


@router.post("/projects/{project_id}/dashboards", status_code=201)
def post_dashboard(project_id: str, body: DashboardIn, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: analysis.create_dashboard(conn, actor, project_id, body.title, body.definition))


@router.get("/dashboards/{dashboard_id}")
def get_dashboard(dashboard_id: str, actor: Actor = Depends(get_actor)):
    return _read(actor, lambda conn: analysis.get_dashboard(conn, dashboard_id))


@router.post("/dashboards/{dashboard_id}/revisions", status_code=201)
def post_dashboard_revision(dashboard_id: str, body: DashboardRevisionIn, actor: Actor = Depends(get_actor)):
    return _write(
        actor,
        lambda conn: analysis.add_dashboard_revision(conn, actor, dashboard_id, body.definition, body.base_revision),
    )


@router.post("/dashboard-revisions/{revision_id}/publish")
def post_publish(revision_id: str, actor: Actor = Depends(get_actor)):
    return _write(actor, lambda conn: analysis.publish_dashboard(conn, actor, revision_id))
