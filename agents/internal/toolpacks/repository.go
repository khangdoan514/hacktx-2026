package toolpacks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

// repository resolves a signed, short-lived run scope through the application
// API. The model never receives the GitHub installation token or chooses the
// owner/repository arguments used by the underlying GitHub tool.
func (r *Runner) repository(ctx context.Context, tool string, args arguments) (any, error) {
	scope := args.str("scope")
	if len(scope) < 16 || len(scope) > 4096 || r.AppAPIURL == "" || r.InternalToken == "" {
		return nil, errors.New("repository scope is unavailable")
	}
	body, _ := json.Marshal(map[string]string{"scope": scope})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.AppAPIURL, "/")+"/api/internal/github-scope", bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("repository scope is unavailable")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(platform.InternalHeader, r.InternalToken)
	response, err := r.Client.Do(request)
	if err != nil {
		return nil, errors.New("repository scope could not be resolved")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("repository scope was denied or expired")
	}
	var grant struct {
		Token    string `json:"token"`
		FullName string `json:"full_name"`
		Branch   string `json:"branch"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&grant) != nil || grant.Token == "" || grant.Branch == "" {
		return nil, errors.New("repository scope response was invalid")
	}
	owner, repo, ok := strings.Cut(grant.FullName, "/")
	if !ok || owner == "" || repo == "" {
		return nil, errors.New("repository scope response was invalid")
	}
	args["owner"], args["repo"] = owner, repo
	if tool == "get_file" {
		args["ref"] = grant.Branch
	}
	delete(args, "scope")
	return r.github(ctx, Credential{AccessToken: grant.Token}, tool, args)
}
