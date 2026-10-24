# Implemented access behavior

This note describes workspaces, projects, invitations, and partner isolation in the FastAPI service. It includes known defects. It does not describe dashboard authorization.

## Workspaces and projects

Creating a workspace inserts the caller's membership before the workspace is read back, because the select policy requires membership and the owner row does not exist yet. The client supplies the workspace id. The creator becomes an active owner.

Workspace roles are `owner`, `administrator`, `member`, and `guest`. Project roles are `owner`, `contributor`, `reviewer`, `viewer`, and `partner`. A workspace must keep one active owner. The database raises `last_owner` if that row would disappear.

Project creation checks the parent project is in the same workspace. A project cannot contain itself (`project_cycle`). Internal cost and internal notes live on `app.project_internal`. Partner projections omit those fields.

`require_workspace` and `require_project` in `backend/src/app/domain/common.py` check the caller's membership role for the command. Project select policies allow an active project member to read the project.

## Invitations

Invitations store a hash of a single-use token and are bound to an email address. Preview does not consume the token. Accept checks the signed-in email, marks the invitation consumed, and inserts workspace membership. Project membership is inserted when the invitation names a project. A pending invitation can be revoked by a workspace owner or administrator.

Accept is allowed for someone who is not yet a member. The membership insert policy permits that when a matching invitation exists for the current email.

## Partners

Partner organizations and contacts are workspace records. A guest is a workspace role. Linking a contact to a user goes through `app.link_partner_contact`, which checks the current email and workspace membership. Suspending a partner is a separate command. Partner project lists are a filtered read, not a second store.

## Known defect: approval scope

`create_approval` in `backend/src/app/domain/work.py` checks that the reviewer is an internal member of the approval's project. It does not check that the target row belongs to that project or workspace. `app.apply_approval_target` loads the approval, checks the caller is `reviewer_id`, then updates `app.tasks` or `app.requirements` by target id and revision only. It does not compare `approval.project_id` or `workspace_id` to the target row. A reviewer who can decide an approval in one project can therefore change a target that lives in another scope if that approval points at it.

## Known defect: membership removal

`update_member` sets `app.workspace_memberships.status` to `removed` and emits `access.revoked` for hint delivery. It does not remove or disable `app.project_memberships`. Project select policies and `allowed_recipient` treat an active project membership as sufficient. After workspace removal returns success, the person can still read the project, its files, and anything else authorized only by that project membership.
