package policy_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/policy"
)

func TestHTTPListPendingAndDecide(t *testing.T) {
	lab := newLab(t)
	ctx := context.Background()
	lab.seedTenant(t, tenantA)
	lab.seedTenant(t, tenantB)
	selfish := user("selfish", tenantA, policy.RoleAIEngineer, policy.RoleReleaseApprover)
	cam := user("cam", tenantA, policy.RoleReleaseApprover)
	bao := user("bao", tenantB, policy.RoleReleaseApprover)
	handler := policy.Handler(lab.eng, func(r *http.Request) (policy.Principal, error) {
		switch r.Header.Get("X-Test-User") {
		case "selfish":
			return selfish, nil
		case "cam":
			return cam, nil
		case "bao":
			return bao, nil
		default:
			return policy.Principal{}, errDenied()
		}
	})

	run := lab.start(t, selfish, tenantA, "run-http")
	key := mustKey(t, tenantA, run.ID, "inv-http", "ticket.write", "digest-http")
	begun, err := lab.eng.BeginProtectedWrite(ctx, selfish, writeReq(run.ID, key, ""))
	if err != nil {
		t.Fatal(err)
	}
	other := lab.start(t, user("bao-eng", tenantB, policy.RoleAIEngineer), tenantB, "run-http-b")
	if _, err := lab.eng.BeginProtectedWrite(ctx, user("bao-eng", tenantB, policy.RoleAIEngineer), writeReq(other.ID, mustKey(t, tenantB, other.ID, "inv-http", "ticket.write", "digest-http-b"), "")); err != nil {
		t.Fatal(err)
	}

	status, body := call(handler, http.MethodGet, "/v1/approvals/pending", "", "")
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d %s", status, body)
	}
	status, body = call(handler, http.MethodGet, "/v1/approvals/pending", "bao", "")
	var listed struct {
		Approvals []struct {
			ID    string `json:"id"`
			RunID string `json:"run_id"`
			State string `json:"state"`
		} `json:"approvals"`
	}
	if status != http.StatusOK || json.Unmarshal([]byte(body), &listed) != nil {
		t.Fatalf("cross-tenant list = %d %s", status, body)
	}
	for _, item := range listed.Approvals {
		if item.ID == begun.Approval.ID || item.RunID == run.ID {
			t.Fatalf("cross-tenant inbox leaked %+v in %s", item, body)
		}
	}

	status, body = call(handler, http.MethodGet, "/v1/approvals/pending", "cam", "")
	if status != http.StatusOK || !strings.Contains(body, begun.Approval.ID) || !strings.Contains(body, `"state":"WAITING"`) {
		t.Fatalf("pending list = %d %s", status, body)
	}
	status, body = call(handler, http.MethodPost, "/v1/approvals/"+begun.Approval.ID+"/decision", "selfish", `{"outcome":"APPROVED"}`)
	if status != http.StatusForbidden || !strings.Contains(body, policy.CodeSelfApproval) {
		t.Fatalf("self decision = %d %s", status, body)
	}
	status, body = call(handler, http.MethodGet, "/v1/approvals/pending", "cam", "")
	if status != http.StatusOK || !strings.Contains(body, begun.Approval.ID) {
		t.Fatalf("still pending = %d %s", status, body)
	}
	status, body = call(handler, http.MethodPost, "/v1/approvals/"+begun.Approval.ID+"/decision", "bao", `{"outcome":"APPROVED"}`)
	if status != http.StatusNotFound || strings.Contains(body, begun.Approval.InputDigest) || strings.Contains(body, policy.CodeCrossTenant) {
		t.Fatalf("cross-tenant decision = %d %s", status, body)
	}
	status, body = call(handler, http.MethodPost, "/v1/approvals/"+begun.Approval.ID+"/decision", "cam", `{"outcome":"APPROVED"}`)
	if status != http.StatusOK || !strings.Contains(body, `"outcome":"APPROVED"`) {
		t.Fatalf("approve = %d %s", status, body)
	}
	status, body = call(handler, http.MethodGet, "/v1/approvals/pending", "cam", "")
	if status != http.StatusOK || strings.Contains(body, begun.Approval.ID) {
		t.Fatalf("list after decision = %d %s", status, body)
	}

	expired, err := lab.eng.BeginProtectedWrite(ctx, selfish, writeReq(run.ID, mustKey(t, tenantA, run.ID, "inv-http", "ticket.write", "digest-http-expire"), ""))
	if err != nil {
		t.Fatal(err)
	}
	lab.clk.Advance(time.Hour)
	status, body = call(handler, http.MethodPost, "/v1/approvals/"+expired.Approval.ID+"/decision", "cam", `{"outcome":"APPROVED"}`)
	if status != http.StatusConflict || !strings.Contains(body, policy.CodeExpired) {
		t.Fatalf("expired decision = %d %s", status, body)
	}
	grant, err := lab.eng.Dispatch(ctx, selfish, expired.Attempt.IdempotencyKey)
	if grant.Granted || grant.Attempt.DispatchCount != 0 || !policy.Is(err, policy.CodeExpired) {
		t.Fatalf("expired dispatch = %+v %v", grant, err)
	}
}

func errDenied() error {
	return &denyError{}
}

type denyError struct{}

func (denyError) Error() string { return "denied" }

func call(handler http.Handler, method, path, userID, body string) (int, string) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if userID != "" {
		req.Header.Set("X-Test-User", userID)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestHTTPDoesNotUseStoreFromContext(t *testing.T) {
	if _, err := policy.OpenPostgres(context.Background(), ""); err == nil || !policy.Is(err, policy.CodeInvalidInput) {
		t.Fatalf("empty url err = %v", err)
	}
}
