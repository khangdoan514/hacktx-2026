package metrics

import "errors"

// ErrUsageInvalid is returned when a token count or a reported cost is negative.
var ErrUsageInvalid = errors.New("metrics: usage counts must be non-negative")
