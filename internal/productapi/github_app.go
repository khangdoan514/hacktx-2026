package productapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type githubApp struct {
	Client *http.Client
	APIURL string
	Issuer string
	KeyPEM []byte
}

func configuredGitHubApp() (*githubApp, error) {
	issuer := os.Getenv("GITHUB_APP_CLIENT_ID")
	if issuer == "" {
		issuer = os.Getenv("GITHUB_APP_ID")
	}
	path := os.Getenv("GITHUB_APP_PRIVATE_KEY_FILE")
	if issuer == "" || path == "" {
		return nil, errors.New("GitHub App credentials are not configured")
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("GitHub App private key could not be read: %w", err)
	}
	apiURL := os.Getenv("GITHUB_API_URL")
	if apiURL == "" {
		apiURL = "https://api.github.com"
	}
	return &githubApp{Client: &http.Client{Timeout: 15 * time.Second}, APIURL: strings.TrimRight(apiURL, "/"), Issuer: issuer, KeyPEM: key}, nil
}

func (a *githubApp) appJWT() (string, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM(a.KeyPEM)
	if err != nil {
		return "", errors.New("GitHub App private key is invalid")
	}
	now := time.Now().UTC()
	claims := jwt.RegisteredClaims{
		Issuer:    a.Issuer,
		IssuedAt:  jwt.NewNumericDate(now.Add(-time.Minute)),
		ExpiresAt: jwt.NewNumericDate(now.Add(8 * time.Minute)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
}

type verifiedGitHubRepository struct {
	RepositoryID   int64
	FullName       string
	InstallationID int64
	AccountID      int64
	AccountLogin   string
}

// verifyRepository checks both the installation and the repo-scoped token at
// GitHub. Client-provided names and installation IDs are not trusted as grants.
func (a *githubApp) verifyRepository(ctx context.Context, installationID, repositoryID int64, fullName string) (verifiedGitHubRepository, error) {
	var result verifiedGitHubRepository
	owner, repo, ok := strings.Cut(fullName, "/")
	if !ok || !validGitHubName(owner) || !validGitHubName(repo) || installationID <= 0 || repositoryID <= 0 {
		return result, errors.New("invalid GitHub repository selection")
	}
	appToken, err := a.appJWT()
	if err != nil {
		return result, err
	}
	var installation struct {
		ID          int64   `json:"id"`
		SuspendedAt *string `json:"suspended_at"`
		Account     struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
		} `json:"account"`
	}
	if err := a.request(ctx, http.MethodGet, fmt.Sprintf("/app/installations/%d", installationID), appToken, nil, &installation); err != nil {
		return result, err
	}
	if installation.ID != installationID || installation.SuspendedAt != nil || installation.Account.ID <= 0 {
		return result, errors.New("GitHub installation is unavailable")
	}
	token, err := a.installationToken(ctx, installationID, repositoryID, appToken)
	if err != nil {
		return result, err
	}
	var repository struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
	}
	if err := a.request(ctx, http.MethodGet, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo), token, nil, &repository); err != nil {
		return result, err
	}
	if repository.ID != repositoryID || !strings.EqualFold(repository.FullName, fullName) {
		return result, errors.New("GitHub repository does not match the selected installation")
	}
	return verifiedGitHubRepository{RepositoryID: repository.ID, FullName: repository.FullName,
		InstallationID: installation.ID, AccountID: installation.Account.ID, AccountLogin: installation.Account.Login}, nil
}

func validGitHubName(value string) bool {
	if value == "" || len(value) > 100 || value == "." || value == ".." {
		return false
	}
	for _, ch := range value {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.') {
			return false
		}
	}
	return true
}

func (a *githubApp) installationToken(ctx context.Context, installationID, repositoryID int64, appToken string) (string, error) {
	var response struct {
		Token string `json:"token"`
	}
	payload := map[string]any{
		"repository_ids": []int64{repositoryID},
		"permissions":    map[string]string{"contents": "read", "issues": "read", "pull_requests": "read"},
	}
	if err := a.request(ctx, http.MethodPost, fmt.Sprintf("/app/installations/%d/access_tokens", installationID), appToken, payload, &response); err != nil {
		return "", err
	}
	if response.Token == "" {
		return "", errors.New("GitHub returned no installation token")
	}
	return response.Token, nil
}

// listBranches uses the same one-repository token as agent tools. Branch
// names are returned by GitHub, never accepted solely from browser input.
func (a *githubApp) listBranches(ctx context.Context, installationID, repositoryID int64, fullName string) ([]string, error) {
	owner, repo, ok := strings.Cut(fullName, "/")
	if !ok || !validGitHubName(owner) || !validGitHubName(repo) {
		return nil, errors.New("invalid GitHub repository")
	}
	jwt, err := a.appJWT()
	if err != nil {
		return nil, err
	}
	token, err := a.installationToken(ctx, installationID, repositoryID, jwt)
	if err != nil {
		return nil, err
	}
	branches := []string{}
	for page := 1; page <= 10; page++ {
		var result []struct {
			Name string `json:"name"`
		}
		path := fmt.Sprintf("/repos/%s/%s/branches?per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(repo), page)
		if err := a.request(ctx, http.MethodGet, path, token, nil, &result); err != nil {
			return nil, err
		}
		for _, branch := range result {
			if branch.Name != "" && len(branch.Name) <= 255 {
				branches = append(branches, branch.Name)
			}
		}
		if len(result) < 100 {
			return branches, nil
		}
	}
	return nil, errors.New("GitHub branch list exceeds the supported page limit")
}

func (a *githubApp) request(ctx context.Context, method, path, token string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, a.APIURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := a.Client.Do(request)
	if err != nil {
		return errors.New("GitHub API could not be reached")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("GitHub API returned %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out)
}
