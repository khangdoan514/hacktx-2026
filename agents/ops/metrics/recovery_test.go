package metrics

import (
	"strings"
	"testing"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

func TestExplainCataloguedCodes(t *testing.T) {
	codes := []contracts.ErrorCode{
		contracts.ErrInvalidInput,
		contracts.ErrNotFound,
		contracts.ErrAgentNotAllowed,
		contracts.ErrDelegationDepthExceeded,
		contracts.ErrToolNotAllowed,
		contracts.ErrBudgetExceeded,
		contracts.ErrTimeout,
		contracts.ErrOutputSchemaInvalid,
		contracts.ErrInternal,
	}
	for _, code := range codes {
		guidance := Explain(string(code))
		if guidance.ErrorCode != string(code) {
			t.Fatalf("code = %s", guidance.ErrorCode)
		}
		if guidance.Cause == "" || guidance.OperatorAction == "" {
			t.Fatalf("%s missing text", code)
		}
		if guidance.Cause == "This error code has no catalogued cause." {
			t.Fatalf("%s fell through to the default cause", code)
		}
		assertNoCredentialMaterial(t, guidance.Cause+" "+guidance.OperatorAction)
	}

	timeout := Explain(string(contracts.ErrTimeout))
	if !strings.Contains(timeout.OperatorAction, "uncertain") {
		t.Fatalf("timeout action = %s", timeout.OperatorAction)
	}
	budget := Explain(string(contracts.ErrBudgetExceeded))
	if !strings.Contains(budget.OperatorAction, "unset") {
		t.Fatalf("budget action = %s", budget.OperatorAction)
	}
	unknown := Explain("NOT_A_CODE")
	if unknown.Cause != "This error code has no catalogued cause." {
		t.Fatalf("unknown cause = %s", unknown.Cause)
	}
	if !strings.Contains(unknown.OperatorAction, "uncertain") || !strings.Contains(unknown.OperatorAction, "committed runs") {
		t.Fatalf("unknown action = %s", unknown.OperatorAction)
	}
}

func assertNoCredentialMaterial(t *testing.T, text string) {
	t.Helper()
	for _, banned := range []string{"postgres://", "Bearer ", "password=", "SUPABASE_SECRET", "sk-"} {
		if strings.Contains(text, banned) {
			t.Fatalf("guidance contains %q: %s", banned, text)
		}
	}
}
