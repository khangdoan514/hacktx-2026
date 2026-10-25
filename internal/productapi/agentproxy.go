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
	"unicode/utf8"

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
	if err := allowAgent(actor, r.Method, r.URL.Path); err != nil {
		writeErr(w, err)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/agent-api/agent-runs" {
		if err := s.prepareAgentRun(w, r, actor); err != nil {
			writeErr(w, err)
			return
		}
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

// prepareAgentRun replaces client-supplied context with rows visible to this
// employee through project RLS. The private process receives only this copy.
func (s *Server) prepareAgentRun(w http.ResponseWriter, r *http.Request, actor platform.Actor) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid run request")
	}
	data, err = s.prepareAgentRunPayload(r.Context(), data, actor)
	if err != nil {
		return err
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	r.ContentLength = int64(len(data))
	return nil
}

// prepareAgentRunPayload also serves the automation worker. Both manual and
// scheduled starts resolve project context using the run owner's current grant.
func (s *Server) prepareAgentRunPayload(ctx context.Context, data []byte, actor platform.Actor) ([]byte, error) {
	var payload map[string]json.RawMessage
	if json.Unmarshal(data, &payload) != nil || payload == nil {
		return nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid run request")
	}
	delete(payload, "shared_context")
	delete(payload, "actor_id")
	var projectID string
	if raw, ok := payload["project_id"]; ok && json.Unmarshal(raw, &projectID) != nil {
		return nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid project ID")
	}
	if projectID != "" {
		var agentID string
		if json.Unmarshal(payload["agent_id"], &agentID) != nil || agentID == "" {
			return nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid agent ID")
		}
		var snippets []map[string]string
		err := s.DB.WithUser(ctx, actor.ID, actor.Email, func(ctx context.Context, tx pgx.Tx) error {
			if _, err := projectAccess(ctx, tx, projectID); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `SELECT id, agent_id, kind, title, body FROM app.project_agent_context
				WHERE project_id=$1 AND deleted_at IS NULL ORDER BY (agent_id=$2) DESC, created_at DESC LIMIT 12`, projectID, agentID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id, sourceAgent, kind, title, body string
				if err := rows.Scan(&id, &sourceAgent, &kind, &title, &body); err != nil {
					return err
				}
				if len(body) > 8192 {
					body = body[:8192]
					for !utf8.ValidString(body) {
						body = body[:len(body)-1]
					}
				}
				snippets = append(snippets, map[string]string{"id": id, "agent_id": sourceAgent, "kind": kind, "title": title, "body": body})
			}
			return rows.Err()
		})
		if err != nil {
			return nil, err
		}
		payload["actor_id"], _ = json.Marshal(actor.ID)
		payload["shared_context"], _ = json.Marshal(snippets)
	} else {
		delete(payload, "project_id")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, platform.Fail(http.StatusBadRequest, "invalid_input", "Invalid run request")
	}
	return data, nil
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

func allowAgent(actor platform.Actor, method, path string) error {
	if method == http.MethodGet || method == http.MethodHead {
		return nil
	}
	if strings.Contains(path, "/approvals/") && strings.HasSuffix(path, "/decision") {
		return organization.Require(actor, "release_approver", "administrator", "security_reviewer")
	}
	return organization.Require(actor, "ai_engineer", "operator", "administrator")
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
