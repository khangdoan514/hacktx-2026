package metrics

import "encoding/json"

// ServiceTarget is a named service objective. Status is TargetUnset and the
// encoding has no numeric objective until a workload is measured.
type ServiceTarget struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

const targetReason = "unset until a representative workload is measured"

// ServiceTargets lists the Pass 8 objectives that must not be invented.
// Concurrency caps in Limiter are operator controls, not measured targets.
func ServiceTargets() []ServiceTarget {
	return []ServiceTarget{
		{Name: "api_latency", Kind: "latency", Status: TargetUnset, Reason: targetReason},
		{Name: "run_throughput", Kind: "throughput", Status: TargetUnset, Reason: targetReason},
		{Name: "recovery_time", Kind: "recovery", Status: TargetUnset, Reason: targetReason},
		{Name: "availability", Kind: "availability", Status: TargetUnset, Reason: targetReason},
		{Name: "run_cost", Kind: "cost", Status: TargetUnset, Reason: targetReason},
	}
}

// Usage totals tokens and, only when every observation included one, a
// caller-supplied cost. A nil cost does not mean zero. This type never
// converts tokens into money.
type Usage struct {
	InputTokens  int
	OutputTokens int
	costMicroUSD int64
	costKnown    bool
	anyMissing   bool
	observations int
}

// Add records one provider observation. costMicroUSD is optional and must
// already be a reported figure; pass nil when the provider did not report cost.
func (u Usage) Add(inputTokens, outputTokens int, costMicroUSD *int64) (Usage, error) {
	if inputTokens < 0 || outputTokens < 0 || (costMicroUSD != nil && *costMicroUSD < 0) {
		return u, ErrUsageInvalid
	}
	u.InputTokens += inputTokens
	u.OutputTokens += outputTokens
	u.observations++
	if costMicroUSD == nil {
		u.anyMissing = true
		u.costKnown = false
		u.costMicroUSD = 0
		return u, nil
	}
	if u.anyMissing {
		return u, nil
	}
	u.costMicroUSD += *costMicroUSD
	u.costKnown = true
	return u, nil
}

// CostMicroUSD reports the summed reported cost. The boolean is false when
// any observation omitted cost, including when no observations were added.
func (u Usage) CostMicroUSD() (int64, bool) {
	if !u.costKnown || u.anyMissing || u.observations == 0 {
		return 0, false
	}
	return u.costMicroUSD, true
}

// MarshalJSON omits cost_micro_usd unless every observation reported a cost,
// so an unset cost cannot be read as zero.
func (u Usage) MarshalJSON() ([]byte, error) {
	payload := map[string]any{
		KeyInputTokens:  u.InputTokens,
		KeyOutputTokens: u.OutputTokens,
		KeyCostStatus:   CostUnset,
	}
	if micro, ok := u.CostMicroUSD(); ok {
		payload[KeyCostStatus] = CostObserved
		payload[KeyCostMicroUSD] = micro
	}
	return json.Marshal(payload)
}
