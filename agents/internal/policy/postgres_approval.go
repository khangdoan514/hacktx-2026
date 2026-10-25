package policy

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func (p *Postgres) CreateProtected(ctx context.Context, approval ApprovalRequest, write WriteAttempt, now time.Time) (ApprovalRequest, WriteAttempt, bool, error) {
	if approval.TenantID != write.TenantID || approval.IdempotencyKey != write.IdempotencyKey || approval.RunID != write.RunID {
		return ApprovalRequest{}, WriteAttempt{}, false, errCode(CodeInvalidInput, "approval and write attempt do not match")
	}
	storedApproval, storedWrite, created, err := p.createProtected(ctx, approval, write, now)
	if uniqueViolation(err) {
		return p.loadProtected(ctx, approval.TenantID, approval.IdempotencyKey, now)
	}
	if err != nil {
		return ApprovalRequest{}, WriteAttempt{}, false, mapStoreErr(err)
	}
	return storedApproval, storedWrite, created, nil
}

func (p *Postgres) createProtected(ctx context.Context, approval ApprovalRequest, write WriteAttempt, now time.Time) (ApprovalRequest, WriteAttempt, bool, error) {
	var storedApproval ApprovalRequest
	var storedWrite WriteAttempt
	var created bool
	err := p.tx(ctx, func(tx pgx.Tx) error {
		existingID, err := approvalIDByKey(ctx, tx, approval.TenantID, approval.IdempotencyKey)
		if err == nil {
			current, attempt, err := p.expireID(ctx, tx, existingID, now)
			if err != nil {
				return err
			}
			storedApproval, storedWrite, created = current, copyWrite(attempt), false
			return nil
		}
		if !Is(err, CodeNotFound) {
			return err
		}
		approval.State = ApprovalWaiting
		approval.ExpiresAt = utc(approval.ExpiresAt)
		approval.CreatedAt = utc(approval.CreatedAt)
		write.State = WriteWaiting
		write.ApprovalID = approval.ID
		write.DispatchCount = 0
		write.DispatchedAt = nil
		write.Outcome = ""
		write.CreatedAt = utc(write.CreatedAt)
		write.UpdatedAt = utc(write.UpdatedAt)
		if err := insertApproval(ctx, tx, approval); err != nil {
			return err
		}
		if err := insertWrite(ctx, tx, write); err != nil {
			return err
		}
		if err := holdRun(ctx, tx, approval.RunID); err != nil {
			return err
		}
		storedApproval, storedWrite, created = approval, copyWrite(write), true
		return nil
	})
	return storedApproval, storedWrite, created, err
}

func (p *Postgres) loadProtected(ctx context.Context, tenantID, key string, now time.Time) (ApprovalRequest, WriteAttempt, bool, error) {
	var storedApproval ApprovalRequest
	var storedWrite WriteAttempt
	err := p.tx(ctx, func(tx pgx.Tx) error {
		id, err := approvalIDByKey(ctx, tx, tenantID, key)
		if err != nil {
			return err
		}
		current, attempt, err := p.expireID(ctx, tx, id, now)
		if err != nil {
			return err
		}
		storedApproval, storedWrite = current, copyWrite(attempt)
		return nil
	})
	if Is(err, CodeNotFound) {
		return ApprovalRequest{}, WriteAttempt{}, false, errCode(CodeConflict, "write idempotency key already exists")
	}
	if err != nil {
		return ApprovalRequest{}, WriteAttempt{}, false, mapStoreErr(err)
	}
	return storedApproval, storedWrite, false, nil
}

func (p *Postgres) Approval(ctx context.Context, id string) (ApprovalRequest, error) {
	if err := p.ready(ctx); err != nil {
		return ApprovalRequest{}, err
	}
	approval, err := approvalAt(ctx, p.pool, id)
	return approval, mapStoreErr(err)
}

func (p *Postgres) ExpireApproval(ctx context.Context, id string, now time.Time) (ApprovalRequest, error) {
	var stored ApprovalRequest
	err := p.tx(ctx, func(tx pgx.Tx) error {
		if _, err := lockApproval(ctx, tx, id); err != nil {
			return err
		}
		current, _, err := p.expireID(ctx, tx, id, now)
		if err != nil {
			return err
		}
		stored = current
		return nil
	})
	if err != nil {
		return ApprovalRequest{}, mapStoreErr(err)
	}
	return stored, nil
}

func (p *Postgres) ListInbox(ctx context.Context, tenantID string, now time.Time) ([]ApprovalRequest, error) {
	var out []ApprovalRequest
	err := p.tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			select id from nmc3.approval_requests
			where tenant_id=$1 and state in ('WAITING', 'APPROVED')
			order by id
			for update`, tenantID)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, id := range ids {
			if _, _, err := p.expireID(ctx, tx, id, now); err != nil {
				return err
			}
		}
		rows, err = tx.Query(ctx, `select `+approvalCols+` from nmc3.approval_requests where tenant_id=$1 and state=$2 order by id`, tenantID, string(ApprovalWaiting))
		if err != nil {
			return err
		}
		defer rows.Close()
		out = []ApprovalRequest{}
		for rows.Next() {
			approval, err := scanApproval(rows.Scan)
			if err != nil {
				return err
			}
			out = append(out, approval)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, mapStoreErr(err)
	}
	if out == nil {
		out = []ApprovalRequest{}
	}
	return out, nil
}

func (p *Postgres) Write(ctx context.Context, tenantID, idempotencyKey string) (WriteAttempt, error) {
	if err := p.ready(ctx); err != nil {
		return WriteAttempt{}, err
	}
	write, err := writeByKey(ctx, p.pool, tenantID, idempotencyKey)
	if err != nil {
		return WriteAttempt{}, mapStoreErr(err)
	}
	return copyWrite(write), nil
}

func (p *Postgres) CommitDecision(ctx context.Context, decision ApprovalDecision, now time.Time) (ApprovalDecision, error) {
	if decision.Outcome != OutcomeApproved && decision.Outcome != OutcomeDenied {
		return ApprovalDecision{}, errCode(CodeInvalidInput, "decision outcome must be APPROVED or DENIED")
	}
	if decision.ApproverID == "" {
		return ApprovalDecision{}, errCode(CodeInvalidInput, "approver is required")
	}
	var stored ApprovalDecision
	var ret error
	err := p.tx(ctx, func(tx pgx.Tx) error {
		approval, err := lockApproval(ctx, tx, decision.RequestID)
		if err != nil {
			return err
		}
		write, err := lockWriteByApproval(ctx, tx, approval.ID)
		if err != nil {
			return err
		}
		decided, err := decisionExists(ctx, tx, approval.ID)
		if err != nil {
			return err
		}
		if decided || (approval.State != ApprovalWaiting && approval.State != ApprovalExpired) {
			ret = errCode(CodeAlreadyDecided, "approval request already has a decision")
			return nil
		}
		approval, write, err = p.expireLocked(ctx, tx, approval, write, now)
		if err != nil {
			return err
		}
		if approval.State == ApprovalExpired {
			ret = errCode(CodeExpired, "approval request has expired")
			return nil
		}
		if decision.ApproverID == approval.RequesterID {
			ret = errCode(CodeSelfApproval, "requester cannot approve their own protected write")
			return nil
		}
		if approval.State != ApprovalWaiting {
			ret = errCode(CodeAlreadyDecided, "approval request already has a decision")
			return nil
		}
		decision.TenantID = approval.TenantID
		decision.RequesterID = approval.RequesterID
		decision.DecidedAt = utc(now)
		if err := insertDecision(ctx, tx, decision); err != nil {
			return err
		}
		if decision.Outcome == OutcomeApproved {
			approval.State = ApprovalApproved
			write.State = WriteReady
		} else {
			approval.State = ApprovalDenied
			write.State = WriteDenied
		}
		write.UpdatedAt = utc(now)
		if err := updateApprovalState(ctx, tx, approval); err != nil {
			return err
		}
		if err := updateWrite(ctx, tx, write); err != nil {
			return err
		}
		if err := releaseRun(ctx, tx, approval.RunID, string(approval.State)); err != nil {
			return err
		}
		stored = decision
		return nil
	})
	if uniqueViolation(err) {
		return ApprovalDecision{}, errCode(CodeAlreadyDecided, "approval request already has a decision")
	}
	if err != nil {
		return ApprovalDecision{}, mapStoreErr(err)
	}
	return stored, ret
}

func (p *Postgres) Dispatch(ctx context.Context, tenantID, idempotencyKey string, now time.Time) (WriteAttempt, bool, error) {
	var write WriteAttempt
	var granted bool
	var ret error
	err := p.tx(ctx, func(tx pgx.Tx) error {
		approvalID, err := approvalIDForWrite(ctx, tx, tenantID, idempotencyKey)
		if err != nil {
			return err
		}
		approval, err := lockApproval(ctx, tx, approvalID)
		if err != nil {
			return err
		}
		write, err = lockWriteByApproval(ctx, tx, approval.ID)
		if err != nil {
			return err
		}
		approval, write, err = p.expireLocked(ctx, tx, approval, write, now)
		if err != nil {
			return err
		}
		switch write.State {
		case WriteUncertain, WriteReconciled:
			ret = errCode(CodeUncertain, "uncertain write requires reconciliation, not a second dispatch")
		case WriteDispatched, WriteSucceeded, WriteFailed:
			ret = errCode(CodeDuplicateWrite, "protected write was already dispatched")
		case WriteDenied:
			ret = errCode(CodeNotDispatchable, "denied approval cannot dispatch")
		case WriteExpired:
			ret = errCode(CodeExpired, "approval request has expired")
		case WriteWaiting:
			ret = errCode(CodeNotDispatchable, "protected write is waiting for approval")
		case WriteReady:
			if approval.State != ApprovalApproved || write.DispatchCount != 0 {
				ret = errCode(CodeNotDispatchable, "protected write is not ready to dispatch")
				return nil
			}
			at := utc(now)
			write.State = WriteDispatched
			write.DispatchCount = 1
			write.DispatchedAt = &at
			write.UpdatedAt = at
			if err := updateWrite(ctx, tx, write); err != nil {
				return err
			}
			granted = true
		default:
			ret = errCode(CodeNotDispatchable, "protected write cannot be dispatched")
		}
		write = copyWrite(write)
		return nil
	})
	if err != nil {
		return WriteAttempt{}, false, mapStoreErr(err)
	}
	return write, granted, ret
}

func (p *Postgres) Timeout(ctx context.Context, tenantID, idempotencyKey string, now time.Time) (WriteAttempt, error) {
	write, ret, err := p.withWrite(ctx, tenantID, idempotencyKey, func(write WriteAttempt) (WriteAttempt, error) {
		switch write.State {
		case WriteUncertain:
			return copyWrite(write), nil
		case WriteDispatched:
			write.State = WriteUncertain
			write.Outcome = ""
			write.UpdatedAt = utc(now)
			return write, nil
		default:
			return copyWrite(write), errCode(CodeNotDispatchable, "only a dispatched write can time out")
		}
	})
	if err != nil {
		return WriteAttempt{}, err
	}
	return write, ret
}

func (p *Postgres) Result(ctx context.Context, tenantID, idempotencyKey, outcome string, now time.Time) (WriteAttempt, error) {
	if outcome != OutcomeSucceeded && outcome != OutcomeFailed {
		return WriteAttempt{}, errCode(CodeInvalidInput, "write result must be SUCCEEDED or FAILED")
	}
	write, ret, err := p.withWrite(ctx, tenantID, idempotencyKey, func(write WriteAttempt) (WriteAttempt, error) {
		switch write.State {
		case WriteUncertain, WriteReconciled:
			return copyWrite(write), errCode(CodeUncertain, "uncertain write requires explicit reconciliation")
		case WriteSucceeded, WriteFailed:
			if write.Outcome == outcome {
				return copyWrite(write), nil
			}
			return copyWrite(write), errCode(CodeConflict, "write result was already recorded")
		case WriteDispatched:
			write.State = WriteState(outcome)
			write.Outcome = outcome
			write.UpdatedAt = utc(now)
			return write, nil
		default:
			return copyWrite(write), errCode(CodeNotDispatchable, "write has not been dispatched")
		}
	})
	if err != nil {
		return WriteAttempt{}, err
	}
	return write, ret
}

func (p *Postgres) Reconcile(ctx context.Context, tenantID, idempotencyKey, outcome string, now time.Time) (WriteAttempt, error) {
	if outcome != OutcomeSucceeded && outcome != OutcomeFailed {
		return WriteAttempt{}, errCode(CodeInvalidInput, "reconciliation outcome must be SUCCEEDED or FAILED")
	}
	write, ret, err := p.withWrite(ctx, tenantID, idempotencyKey, func(write WriteAttempt) (WriteAttempt, error) {
		switch write.State {
		case WriteReconciled:
			if write.Outcome == outcome {
				return copyWrite(write), nil
			}
			return copyWrite(write), errCode(CodeConflict, "write was already reconciled")
		case WriteUncertain:
			if write.DispatchCount != 1 {
				return copyWrite(write), errCode(CodeNotDispatchable, "uncertain write is missing its dispatch")
			}
			write.State = WriteReconciled
			write.Outcome = outcome
			write.UpdatedAt = utc(now)
			return write, nil
		default:
			return copyWrite(write), errCode(CodeNotDispatchable, "only an uncertain write can be reconciled")
		}
	})
	if err != nil {
		return WriteAttempt{}, err
	}
	return write, ret
}

// withWrite locks the approval, then the write, and saves the attempt when
// mutate changes it. A business error still commits so a prior lock is not
// held, and the current attempt is returned with that error.
func (p *Postgres) withWrite(ctx context.Context, tenantID, key string, mutate func(WriteAttempt) (WriteAttempt, error)) (WriteAttempt, error, error) {
	var write WriteAttempt
	var ret error
	err := p.tx(ctx, func(tx pgx.Tx) error {
		approvalID, err := approvalIDForWrite(ctx, tx, tenantID, key)
		if err != nil {
			return err
		}
		if _, err := lockApproval(ctx, tx, approvalID); err != nil {
			return err
		}
		current, err := lockWriteByApproval(ctx, tx, approvalID)
		if err != nil {
			return err
		}
		next, business := mutate(current)
		ret = business
		if business == nil && writeChanged(current, next) {
			if err := updateWrite(ctx, tx, next); err != nil {
				return err
			}
		}
		write = copyWrite(next)
		return nil
	})
	if err != nil {
		return WriteAttempt{}, nil, mapStoreErr(err)
	}
	return write, ret, nil
}

func writeChanged(before, after WriteAttempt) bool {
	return before.State != after.State || before.Outcome != after.Outcome || before.DispatchCount != after.DispatchCount || before.UpdatedAt != after.UpdatedAt
}

func (p *Postgres) expireID(ctx context.Context, tx pgx.Tx, id string, now time.Time) (ApprovalRequest, WriteAttempt, error) {
	approval, err := lockApproval(ctx, tx, id)
	if err != nil {
		return ApprovalRequest{}, WriteAttempt{}, err
	}
	write, err := lockWriteByApproval(ctx, tx, id)
	if err != nil {
		return ApprovalRequest{}, WriteAttempt{}, err
	}
	return p.expireLocked(ctx, tx, approval, write, now)
}

func (p *Postgres) expireLocked(ctx context.Context, tx pgx.Tx, approval ApprovalRequest, write WriteAttempt, now time.Time) (ApprovalRequest, WriteAttempt, error) {
	nextApproval, nextWrite, changed := expirePair(approval, write, now)
	if !changed {
		return approval, write, nil
	}
	if err := updateApprovalState(ctx, tx, nextApproval); err != nil {
		return ApprovalRequest{}, WriteAttempt{}, err
	}
	if nextWrite.ID != "" && nextWrite.State != write.State {
		nextWrite.UpdatedAt = utc(nextWrite.UpdatedAt)
		if err := updateWrite(ctx, tx, nextWrite); err != nil {
			return ApprovalRequest{}, WriteAttempt{}, err
		}
	}
	if err := releaseRun(ctx, tx, nextApproval.RunID, string(ApprovalExpired)); err != nil {
		return ApprovalRequest{}, WriteAttempt{}, err
	}
	return nextApproval, nextWrite, nil
}

const approvalCols = `id, tenant_id, run_id, invocation_id, tool_id, tool_version, idempotency_key, policy_id, policy_revision, requester_id, state, input_digest, expires_at, created_at`

func approvalAt(ctx context.Context, q querier, id string) (ApprovalRequest, error) {
	row := q.QueryRow(ctx, `select `+approvalCols+` from nmc3.approval_requests where id=$1`, id)
	approval, err := scanApproval(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return ApprovalRequest{}, errCode(CodeNotFound, "approval request not found")
	}
	return approval, err
}

func lockApproval(ctx context.Context, tx pgx.Tx, id string) (ApprovalRequest, error) {
	row := tx.QueryRow(ctx, `select `+approvalCols+` from nmc3.approval_requests where id=$1 for update`, id)
	approval, err := scanApproval(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return ApprovalRequest{}, errCode(CodeNotFound, "approval request not found")
	}
	return approval, err
}

func scanApproval(scan func(dest ...any) error) (ApprovalRequest, error) {
	var approval ApprovalRequest
	var state string
	err := scan(&approval.ID, &approval.TenantID, &approval.RunID, &approval.InvocationID, &approval.ToolID, &approval.ToolVersion, &approval.IdempotencyKey, &approval.PolicyID, &approval.PolicyRevision, &approval.RequesterID, &state, &approval.InputDigest, &approval.ExpiresAt, &approval.CreatedAt)
	if err != nil {
		return ApprovalRequest{}, err
	}
	approval.State = ApprovalState(state)
	approval.ExpiresAt = approval.ExpiresAt.UTC()
	approval.CreatedAt = approval.CreatedAt.UTC()
	return approval, nil
}

func insertApproval(ctx context.Context, tx pgx.Tx, approval ApprovalRequest) error {
	_, err := tx.Exec(ctx, `
		insert into nmc3.approval_requests (
			id, tenant_id, run_id, invocation_id, tool_id, tool_version, idempotency_key,
			policy_id, policy_revision, requester_id, state, input_digest, expires_at, created_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		approval.ID, approval.TenantID, approval.RunID, approval.InvocationID, approval.ToolID, approval.ToolVersion, approval.IdempotencyKey,
		approval.PolicyID, approval.PolicyRevision, approval.RequesterID, string(approval.State), approval.InputDigest, utc(approval.ExpiresAt), utc(approval.CreatedAt))
	return err
}

func updateApprovalState(ctx context.Context, tx pgx.Tx, approval ApprovalRequest) error {
	_, err := tx.Exec(ctx, `update nmc3.approval_requests set state=$2 where id=$1`, approval.ID, string(approval.State))
	return err
}

func approvalIDByKey(ctx context.Context, q querier, tenantID, key string) (string, error) {
	var id string
	err := q.QueryRow(ctx, `select id from nmc3.approval_requests where tenant_id=$1 and idempotency_key=$2`, tenantID, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errCode(CodeNotFound, "approval request not found")
	}
	return id, err
}

func approvalIDForWrite(ctx context.Context, q querier, tenantID, key string) (string, error) {
	var id string
	err := q.QueryRow(ctx, `select approval_id from nmc3.write_attempts where tenant_id=$1 and idempotency_key=$2`, tenantID, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errCode(CodeNotFound, "protected write not found")
	}
	return id, err
}

const writeCols = `id, tenant_id, idempotency_key, run_id, invocation_id, tool_id, tool_version, approval_id, state, dispatch_count, dispatched_at, outcome, created_at, updated_at`

func writeByKey(ctx context.Context, q querier, tenantID, key string) (WriteAttempt, error) {
	row := q.QueryRow(ctx, `select `+writeCols+` from nmc3.write_attempts where tenant_id=$1 and idempotency_key=$2`, tenantID, key)
	write, err := scanWrite(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return WriteAttempt{}, errCode(CodeNotFound, "protected write not found")
	}
	return write, err
}

func lockWriteByApproval(ctx context.Context, tx pgx.Tx, approvalID string) (WriteAttempt, error) {
	rows, err := tx.Query(ctx, `select `+writeCols+` from nmc3.write_attempts where approval_id=$1 for update`, approvalID)
	if err != nil {
		return WriteAttempt{}, err
	}
	defer rows.Close()
	var write WriteAttempt
	var n int
	for rows.Next() {
		n++
		if n > 1 {
			return WriteAttempt{}, errCode(CodeConflict, "approval has more than one write attempt")
		}
		write, err = scanWrite(rows.Scan)
		if err != nil {
			return WriteAttempt{}, err
		}
	}
	if err := rows.Err(); err != nil {
		return WriteAttempt{}, err
	}
	if n == 0 {
		return WriteAttempt{}, errCode(CodeNotFound, "protected write not found")
	}
	return write, nil
}

func scanWrite(scan func(dest ...any) error) (WriteAttempt, error) {
	var write WriteAttempt
	var state string
	err := scan(&write.ID, &write.TenantID, &write.IdempotencyKey, &write.RunID, &write.InvocationID, &write.ToolID, &write.ToolVersion, &write.ApprovalID, &state, &write.DispatchCount, &write.DispatchedAt, &write.Outcome, &write.CreatedAt, &write.UpdatedAt)
	if err != nil {
		return WriteAttempt{}, err
	}
	write.State = WriteState(state)
	write.CreatedAt = write.CreatedAt.UTC()
	write.UpdatedAt = write.UpdatedAt.UTC()
	if write.DispatchedAt != nil {
		at := write.DispatchedAt.UTC()
		write.DispatchedAt = &at
	}
	return write, nil
}

func insertWrite(ctx context.Context, tx pgx.Tx, write WriteAttempt) error {
	_, err := tx.Exec(ctx, `
		insert into nmc3.write_attempts (
			id, tenant_id, idempotency_key, run_id, invocation_id, tool_id, tool_version, approval_id,
			state, dispatch_count, dispatched_at, outcome, created_at, updated_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		write.ID, write.TenantID, write.IdempotencyKey, write.RunID, write.InvocationID, write.ToolID, write.ToolVersion, write.ApprovalID,
		string(write.State), write.DispatchCount, utcPtr(write.DispatchedAt), write.Outcome, utc(write.CreatedAt), utc(write.UpdatedAt))
	return err
}

func updateWrite(ctx context.Context, tx pgx.Tx, write WriteAttempt) error {
	_, err := tx.Exec(ctx, `
		update nmc3.write_attempts
		set state=$2, dispatch_count=$3, dispatched_at=$4, outcome=$5, updated_at=$6
		where id=$1`,
		write.ID, string(write.State), write.DispatchCount, utcPtr(write.DispatchedAt), write.Outcome, utc(write.UpdatedAt))
	return err
}

func decisionExists(ctx context.Context, q querier, requestID string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `select exists(select 1 from nmc3.approval_decisions where request_id=$1)`, requestID).Scan(&ok)
	return ok, err
}

func insertDecision(ctx context.Context, tx pgx.Tx, decision ApprovalDecision) error {
	_, err := tx.Exec(ctx, `
		insert into nmc3.approval_decisions (
			id, request_id, tenant_id, approver_id, requester_id, outcome, decided_at
		) values ($1,$2,$3,$4,$5,$6,$7)`,
		decision.ID, decision.RequestID, decision.TenantID, decision.ApproverID, decision.RequesterID, decision.Outcome, utc(decision.DecidedAt))
	return err
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	at := utc(*t)
	return &at
}
