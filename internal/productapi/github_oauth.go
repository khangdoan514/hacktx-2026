package productapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type githubOAuth struct {
	ClientID     string
	ClientSecret string
	CallbackURL  string
	AuthorizeURL string
	TokenURL     string
	APIURL       string
	Client       *http.Client
}

func configuredGitHubOAuth() (githubOAuth, error) {
	client := githubOAuth{
		ClientID: os.Getenv("GITHUB_APP_CLIENT_ID"), ClientSecret: os.Getenv("GITHUB_APP_CLIENT_SECRET"),
		CallbackURL: os.Getenv("GITHUB_APP_CALLBACK_URL"), AuthorizeURL: os.Getenv("GITHUB_OAUTH_AUTHORIZE_URL"),
		TokenURL: os.Getenv("GITHUB_OAUTH_TOKEN_URL"), APIURL: os.Getenv("GITHUB_API_URL"),
		Client: &http.Client{Timeout: 15 * time.Second},
	}
	if client.AuthorizeURL == "" {
		client.AuthorizeURL = "https://github.com/login/oauth/authorize"
	}
	if client.TokenURL == "" {
		client.TokenURL = "https://github.com/login/oauth/access_token"
	}
	if client.APIURL == "" {
		client.APIURL = "https://api.github.com"
	}
	parsed, err := url.Parse(client.CallbackURL)
	if client.ClientID == "" || client.ClientSecret == "" || err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Path != "/api/v1/github/oauth/callback" {
		return githubOAuth{}, errors.New("GitHub App user authorization is not configured")
	}
	return client, nil
}

func githubPKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (o githubOAuth) authorize(state, verifier string) string {
	query := url.Values{"client_id": {o.ClientID}, "redirect_uri": {o.CallbackURL}, "state": {state},
		"code_challenge": {githubPKCEChallenge(verifier)}, "code_challenge_method": {"S256"}}
	return o.AuthorizeURL + "?" + query.Encode()
}

func (o githubOAuth) exchange(ctx context.Context, code, verifier string) (string, error) {
	form := url.Values{"client_id": {o.ClientID}, "client_secret": {o.ClientSecret}, "code": {code},
		"redirect_uri": {o.CallbackURL}, "code_verifier": {verifier}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, o.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := o.Client.Do(request)
	if err != nil {
		return "", errors.New("GitHub authorization could not be reached")
	}
	defer response.Body.Close()
	var body struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body) != nil || body.AccessToken == "" || body.Error != "" {
		return "", errors.New("GitHub authorization did not return a user token")
	}
	return body.AccessToken, nil
}

type githubCandidate struct {
	InstallationID int64  `json:"installation_id"`
	AccountID      int64  `json:"account_id"`
	AccountLogin   string `json:"account_login"`
	RepositoryID   int64  `json:"repository_id"`
	FullName       string `json:"full_name"`
}

// candidateRepositories uses a GitHub App user token. GitHub intersects the
// user's grants with the App's selected repositories. We discard the token
// after this request and store only the short-lived candidate list.
func (o githubOAuth) candidateRepositories(ctx context.Context, token string) ([]githubCandidate, error) {
	api := &githubApp{Client: o.Client, APIURL: strings.TrimRight(o.APIURL, "/")}
	candidates := []githubCandidate{}
	for page := 1; page <= 10; page++ {
		var installations struct {
			Installations []struct {
				ID          int64   `json:"id"`
				SuspendedAt *string `json:"suspended_at"`
				Account     struct {
					ID    int64  `json:"id"`
					Login string `json:"login"`
				} `json:"account"`
			} `json:"installations"`
		}
		path := "/user/installations?per_page=100&page=" + strconv.Itoa(page)
		if err := api.request(ctx, http.MethodGet, path, token, nil, &installations); err != nil {
			return nil, err
		}
		for _, install := range installations.Installations {
			if install.ID <= 0 || install.Account.ID <= 0 || install.SuspendedAt != nil {
				continue
			}
			for repoPage := 1; repoPage <= 10; repoPage++ {
				var repos struct {
					Repositories []struct {
						ID       int64  `json:"id"`
						FullName string `json:"full_name"`
					} `json:"repositories"`
				}
				path := fmt.Sprintf("/user/installations/%d/repositories?per_page=100&page=%d", install.ID, repoPage)
				if err := api.request(ctx, http.MethodGet, path, token, nil, &repos); err != nil {
					return nil, err
				}
				for _, repo := range repos.Repositories {
					owner, name, ok := strings.Cut(repo.FullName, "/")
					if repo.ID > 0 && ok && validGitHubName(owner) && validGitHubName(name) {
						candidates = append(candidates, githubCandidate{InstallationID: install.ID,
							AccountID: install.Account.ID, AccountLogin: install.Account.Login,
							RepositoryID: repo.ID, FullName: repo.FullName})
					}
				}
				if len(repos.Repositories) < 100 {
					break
				}
				if repoPage == 10 {
					return nil, errors.New("GitHub repository selection exceeds the supported page limit")
				}
			}
		}
		if len(installations.Installations) < 100 {
			return candidates, nil
		}
	}
	return nil, errors.New("GitHub installation selection exceeds the supported page limit")
}
