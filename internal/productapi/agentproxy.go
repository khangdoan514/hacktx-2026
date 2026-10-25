package productapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/organization"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

// proxyAgent is the door for the console and for CLI clients. It checks the
// same employee token as the rest of cmd/api, then calls the private agent
// process. The browser and the CLI do not connect to that process.
func (s *Server) proxyAgent(w http.ResponseWriter, r *http.Request) {
	identity, err := s.bearerIdentity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var actor platform.Actor
	err = s.DB.WithUser(r.Context(), identity.UserID, identity.Email, func(ctx context.Context, tx pgx.Tx) error {
		loaded, loadErr := organization.Load(ctx, tx, identity.UserID, identity.Email)
		actor = loaded
		return loadErr
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := allowAgent(actor, r.Method, r.URL.Path, runAgentID(r)); err != nil {
		writeErr(w, err)
		return
	}
	upstream, err := url.Parse(s.Config.AgentAPIURL)
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		writeErr(w, platform.Fail(http.StatusBadGateway, "bad_gateway", "The agent API is not configured"))
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		writeErr(w, platform.Fail(http.StatusBadGateway, "bad_gateway", "The agent API is not reachable"))
	}
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		req.URL.Path = agentUpstreamPath(r.URL.Path)
		req.URL.RawQuery = r.URL.RawQuery
		req.Host = upstream.Host
		stampEmployee(req, actor)
	}
	proxy.ServeHTTP(w, r)
}

func (s *Server) bearerIdentity(r *http.Request) (platform.Identity, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return platform.Identity{}, platform.Fail(http.StatusUnauthorized, "unauthenticated", "Authentication required")
	}
	token, err := platform.Bearer(header)
	if err != nil {
		return platform.Identity{}, err
	}
	identity, err := platform.Verify(s.Config, token, s.Keys)
	if err != nil {
		return platform.Identity{}, err
	}
	return organization.EmployeeIdentity(r.Context(), s.DB, identity.UserID, identity.Email)
}

func allowAgent(actor platform.Actor, method, path, agentID string) error {
	if path == "/agent-api/threads" || strings.HasPrefix(path, "/agent-api/threads/") {
		return organization.Require(actor, "requester", "ai_engineer", "operator", "administrator")
	}
	if method == http.MethodGet || method == http.MethodHead {
		return nil
	}
	if strings.Contains(path, "/approvals/") && strings.HasSuffix(path, "/decision") {
		return organization.Require(actor, "release_approver", "administrator", "security_reviewer")
	}
	if actor.Has("analyst") && !actor.Has("ai_engineer") && !actor.Has("operator") && !actor.Has("administrator") {
		if strings.HasSuffix(path, "/agent-runs") && (agentID == "partner-analytics" || agentID == "internal-analytics") {
			return nil
		}
		return platform.Fail(http.StatusForbidden, "forbidden", "You cannot do that")
	}
	return organization.Require(actor, "ai_engineer", "operator", "administrator")
}

func runAgentID(r *http.Request) string {
	if r.Body == nil || r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/agent-runs") {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return ""
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	var payload struct {
		AgentID string `json:"agent_id"`
	}
	_ = json.Unmarshal(body, &payload)
	return payload.AgentID
}

func agentUpstreamPath(path string) string {
	rest := strings.TrimPrefix(path, "/agent-api")
	if rest == "" || rest == "/" {
		return "/v1"
	}
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}
	return "/v1" + rest
}

func stampEmployee(req *http.Request, actor platform.Actor) {
	for _, key := range []string{
		"Authorization", "X-NMC3-Console", platform.ActorHeader,
		platform.InternalHeader, platform.UserHeader, platform.EmailHeader,
		platform.TenantHeader, platform.RolesHeader,
	} {
		req.Header.Del(key)
	}
	req.Header.Set(platform.ActorHeader, actor.Email)
	req.Header.Set(platform.UserHeader, actor.ID)
	req.Header.Set(platform.EmailHeader, actor.Email)
	req.Header.Set(platform.TenantHeader, actor.OrgID)
	req.Header.Set(platform.RolesHeader, strings.Join(actor.Roles, ","))
	if token := platform.InternalToken(); token != "" {
		req.Header.Set(platform.InternalHeader, token)
	}
}
