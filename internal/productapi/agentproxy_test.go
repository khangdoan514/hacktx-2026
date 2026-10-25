package productapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

func TestAgentProxyRequiresBearer(t *testing.T) {
	server := &Server{Config: platform.Config{Environment: "test", AgentAPIURL: "http://127.0.0.1:9"}}
	rec := httptest.NewRecorder()
	server.proxyAgent(rec, httptest.NewRequest(http.MethodGet, "/agent-api/agents", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestAgentUpstreamPath(t *testing.T) {
	if got := agentUpstreamPath("/agent-api/definitions"); got != "/v1/definitions" {
		t.Fatal(got)
	}
	if got := agentUpstreamPath("/agent-api/approvals/abc/decision"); got != "/v1/approvals/abc/decision" {
		t.Fatal(got)
	}
}

func TestStampEmployeeReplacesClientIdentity(t *testing.T) {
	t.Setenv("NMC3_INTERNAL_TOKEN", "local-internal")
	req := httptest.NewRequest(http.MethodPost, "/agent-api/definitions/publish", nil)
	req.Header.Set("Authorization", "Bearer client-token")
	req.Header.Set(platform.ActorHeader, "local")
	req.Header.Set(platform.UserHeader, "someone-else")
	stampEmployee(req, platform.Actor{
		ID: "20000000-0000-4000-8000-000000000002", Email: "bao.ai@nmc2.test",
		OrgID: platform.OrgID, Roles: []string{"ai_engineer"},
	})
	if req.Header.Get("Authorization") != "" || req.Header.Get(platform.ActorHeader) != "bao.ai@nmc2.test" {
		t.Fatalf("headers %v", req.Header)
	}
	if req.Header.Get(platform.UserHeader) != "20000000-0000-4000-8000-000000000002" || req.Header.Get(platform.InternalHeader) != "local-internal" {
		t.Fatalf("identity headers %v", req.Header)
	}
	if req.Header.Get(platform.RolesHeader) != "ai_engineer" || req.Header.Get(platform.TenantHeader) != platform.OrgID {
		t.Fatalf("role headers %v", req.Header)
	}
}

func TestAllowAgentUsesEmployeeRoles(t *testing.T) {
	requester := platform.Actor{Roles: []string{"requester"}}
	engineer := platform.Actor{Roles: []string{"ai_engineer"}}
	approver := platform.Actor{Roles: []string{"release_approver"}}
	if err := allowAgent(requester, http.MethodGet, "/agent-api/agents", ""); err != nil {
		t.Fatal(err)
	}
	if err := allowAgent(requester, http.MethodPost, "/agent-api/agent-runs", "partner-analytics"); err == nil {
		t.Fatal("requester started a run")
	}
	if err := allowAgent(requester, http.MethodGet, "/agent-api/threads", ""); err != nil {
		t.Fatal("requester cannot read threads")
	}
	if err := allowAgent(requester, http.MethodPost, "/agent-api/threads/thread-id/messages", ""); err != nil {
		t.Fatal("requester cannot send a thread message")
	}
	if err := allowAgent(engineer, http.MethodGet, "/agent-api/threads", ""); err != nil {
		t.Fatal(err)
	}
	if err := allowAgent(engineer, http.MethodPost, "/agent-api/threads/thread-id/messages", ""); err != nil {
		t.Fatal(err)
	}
	if err := allowAgent(engineer, http.MethodPost, "/agent-api/agent-runs", "vendor-orchestrator"); err != nil {
		t.Fatal(err)
	}
	if err := allowAgent(engineer, http.MethodPost, "/agent-api/approvals/one/decision", ""); err == nil {
		t.Fatal("engineer decided an approval")
	}
	if err := allowAgent(approver, http.MethodPost, "/agent-api/approvals/one/decision", ""); err != nil {
		t.Fatal(err)
	}
	analyst := platform.Actor{Roles: []string{"analyst"}}
	if err := allowAgent(analyst, http.MethodPost, "/agent-api/agent-runs", "partner-analytics"); err != nil {
		t.Fatal(err)
	}
	if err := allowAgent(analyst, http.MethodPost, "/agent-api/agent-runs", "internal-analytics"); err != nil {
		t.Fatal(err)
	}
	if err := allowAgent(analyst, http.MethodPost, "/agent-api/agent-runs", "vendor-orchestrator"); err == nil {
		t.Fatal("analyst started a vendor run")
	}
	if err := allowAgent(analyst, http.MethodPost, "/agent-api/approvals/one/decision", ""); err == nil {
		t.Fatal("analyst decided an approval")
	}
}
