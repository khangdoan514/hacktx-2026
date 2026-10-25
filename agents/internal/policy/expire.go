package policy

import "time"

// expirePair applies inclusive expiry (now >= expires_at). changed is true
// when the approval moves to EXPIRED. A write that already dispatched is left
// alone so expiry cannot grant or erase that attempt.
func expirePair(approval ApprovalRequest, write WriteAttempt, now time.Time) (ApprovalRequest, WriteAttempt, bool) {
	if approval.State == ApprovalExpired {
		return approval, write, false
	}
	if now.Before(approval.ExpiresAt) {
		return approval, write, false
	}
	switch write.State {
	case WriteDispatched, WriteSucceeded, WriteFailed, WriteUncertain, WriteReconciled:
		return approval, write, false
	}
	if approval.State != ApprovalWaiting && approval.State != ApprovalApproved {
		return approval, write, false
	}
	approval.State = ApprovalExpired
	if write.ID != "" && (write.State == WriteWaiting || write.State == WriteReady) {
		write.State = WriteExpired
		write.UpdatedAt = now
	}
	return approval, write, true
}
