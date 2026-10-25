package metrics

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestServiceTargetsStayUnset(t *testing.T) {
	targets := ServiceTargets()
	seen := map[string]bool{}
	for _, target := range targets {
		seen[target.Kind] = true
		if target.Status != TargetUnset {
			t.Fatalf("%s status = %s", target.Name, target.Status)
		}
		if target.Reason != "unset until a representative workload is measured" {
			t.Fatalf("%s reason = %q", target.Name, target.Reason)
		}
		if strings.ContainsAny(target.Reason, "0123456789") {
			t.Fatalf("%s reason contains a number: %s", target.Name, target.Reason)
		}
		encoded, err := json.Marshal(target)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		for key, value := range decoded {
			if _, ok := value.(float64); ok {
				t.Fatalf("%s field %s is numeric (%v)", target.Name, key, value)
			}
		}
	}
	for _, kind := range []string{"latency", "throughput", "recovery", "availability", "cost"} {
		if !seen[kind] {
			t.Fatalf("missing %s target", kind)
		}
	}
}

func TestUsageDoesNotInventCost(t *testing.T) {
	var total Usage
	var err error
	total, err = total.Add(10, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	total, err = total.Add(3, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if total.InputTokens != 13 || total.OutputTokens != 5 {
		t.Fatalf("tokens = %d/%d", total.InputTokens, total.OutputTokens)
	}
	if _, ok := total.CostMicroUSD(); ok {
		t.Fatal("missing observations produced a cost")
	}
	encoded, err := json.Marshal(total)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), KeyCostMicroUSD) {
		t.Fatalf("unset cost was encoded: %s", encoded)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded[KeyCostStatus] != CostUnset {
		t.Fatalf("status = %#v", decoded[KeyCostStatus])
	}

	reported := int64(125)
	other := int64(25)
	observed, err := Usage{}.Add(2, 2, &reported)
	if err != nil {
		t.Fatal(err)
	}
	observed, err = observed.Add(1, 1, &other)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := observed.CostMicroUSD()
	if !ok || got != 150 {
		t.Fatalf("observed cost = %d ok=%v", got, ok)
	}
	mixed, err := observed.Add(1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := mixed.CostMicroUSD(); ok {
		t.Fatal("a later missing cost still published a sum")
	}
	if _, err := (Usage{}).Add(-1, 0, nil); !errors.Is(err, ErrUsageInvalid) {
		t.Fatalf("negative tokens = %v", err)
	}
	negative := int64(-1)
	if _, err := (Usage{}).Add(0, 0, &negative); !errors.Is(err, ErrUsageInvalid) {
		t.Fatalf("negative cost = %v", err)
	}
}
