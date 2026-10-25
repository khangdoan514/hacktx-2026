package durable

import "errors"

var (
	ErrInvalid             = errors.New("durable: invalid durable request")
	ErrNotFound            = errors.New("durable: not found")
	ErrLeaseHeld           = errors.New("durable: worker lease is held")
	ErrNeedsRecovery       = errors.New("durable: expired lease needs explicit recovery")
	ErrStaleLease          = errors.New("durable: stale worker lease")
	ErrAttemptClaimed      = errors.New("durable: active attempt is already claimed")
	ErrAlreadyCompleted    = errors.New("durable: invocation already completed")
	ErrIdempotencyConflict = errors.New("durable: idempotency key reused with a different request")
	ErrCancelled           = errors.New("durable: run is cancelled")
	ErrUncertain           = errors.New("durable: operation outcome is uncertain")
	ErrProtectedWrite      = errors.New("durable: protected writes are not dispatched")
	ErrRetriesExhausted    = errors.New("durable: read retries exhausted")
	ErrLostResponse        = errors.New("durable: lost response")
	ErrOutcomeUnknown      = errors.New("durable: outcome unknown")
	ErrWorkerStopped       = errors.New("durable: worker stopped")
	ErrRunTerminal         = errors.New("durable: run is already terminal")
)
