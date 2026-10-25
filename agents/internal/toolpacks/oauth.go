package toolpacks

import (
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
)

// Provider is the OAuth client for a pack. The client id and secret come from
// the environment so they are never in git.
type Provider struct {
	AuthURL      string
	TokenURL     string
	Scope        string
	ClientID     string
	ClientSecret string
	Extra        url.Values // added to the authorize URL
	JSONBasic    bool       // Notion uses JSON and HTTP Basic instead of form credentials
	SlackUser    bool
	PKCE         bool
}

// Configured reports whether the deployment registered an OAuth client.
func (p Provider) Configured() bool { return p.ClientID != "" && p.ClientSecret != "" }

// ProviderFor reads the OAuth client for a pack from the environment:
// GITHUB_CLIENT_ID/SECRET and GOOGLE_CLIENT_ID/SECRET.
func ProviderFor(pack string) (Provider, bool) {
	switch pack {
	case "github":
		return Provider{
			AuthURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token",
			// GitHub has no read-only scope for private repositories.
			Scope:    "repo read:user",
			ClientID: os.Getenv("GITHUB_CLIENT_ID"), ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		}, true
	case "gdrive":
		return Provider{
			AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token",
			Scope:    "https://www.googleapis.com/auth/drive.readonly",
			ClientID: os.Getenv("GOOGLE_CLIENT_ID"), ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
			Extra: url.Values{"access_type": {"offline"}, "prompt": {"consent"}, "response_type": {"code"}},
		}, true
	case "notion":
		return Provider{
			AuthURL: "https://api.notion.com/v1/oauth/authorize", TokenURL: "https://api.notion.com/v1/oauth/token",
			ClientID: os.Getenv("NOTION_CLIENT_ID"), ClientSecret: os.Getenv("NOTION_CLIENT_SECRET"),
			Extra: url.Values{"owner": {"user"}, "response_type": {"code"}}, JSONBasic: true,
		}, true
	case "slack":
		scopes := strings.TrimSpace(os.Getenv("SLACK_USER_SCOPES"))
		if scopes == "" {
			scopes = "channels:read,channels:history,search:read.public"
		}
		return Provider{
			AuthURL: "https://slack.com/oauth/v2/authorize", TokenURL: "https://slack.com/api/oauth.v2.access",
			ClientID: os.Getenv("SLACK_CLIENT_ID"), ClientSecret: os.Getenv("SLACK_CLIENT_SECRET"),
			Extra: url.Values{"user_scope": {scopes}}, SlackUser: true, PKCE: os.Getenv("SLACK_OAUTH_PKCE") != "false",
		}, true
	}
	return Provider{}, false
}

func (p Provider) AuthorizeURL(redirect, state string) string {
	q := url.Values{"client_id": {p.ClientID}, "redirect_uri": {redirect}, "scope": {p.Scope}, "state": {state}}
	if p.Scope == "" {
		q.Del("scope")
	}
	for k, v := range p.Extra {
		q[k] = v
	}
	return p.AuthURL + "?" + q.Encode()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
	OK           bool   `json:"ok"`
	TokenType    string `json:"token_type"`
	AuthedUser   struct {
		ID           string `json:"id"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	} `json:"authed_user"`
}

func (p Provider) token(ctx context.Context, client *http.Client, form url.Values) (Credential, error) {
	if p.PKCE {
		form.Del("client_secret")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Credential{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if p.JSONBasic {
		payload := map[string]string{}
		for k := range form {
			if k != "client_id" && k != "client_secret" {
				payload[k] = form.Get(k)
			}
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return Credential{}, err
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(string(body)))
		if err != nil {
			return Credential{}, err
		}
		req.SetBasicAuth(p.ClientID, p.ClientSecret)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Notion-Version", notionVersion)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return Credential{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return Credential{}, fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}
	if p.SlackUser {
		if !tr.OK {
			return Credential{}, errors.New("Slack refused the sign-in. Check the app's scopes and OAuth settings")
		}
		if form.Get("grant_type") == "refresh_token" {
			if tr.TokenType != "user" {
				return Credential{}, errors.New("Slack refresh did not return a user token")
			}
		} else {
			if tr.AuthedUser.AccessToken == "" || tr.AuthedUser.ID == "" || tr.AuthedUser.TokenType != "user" {
				return Credential{}, errors.New("Slack did not return an employee token. Configure User Token Scopes")
			}
			tr.AccessToken, tr.RefreshToken, tr.ExpiresIn = tr.AuthedUser.AccessToken, tr.AuthedUser.RefreshToken, tr.AuthedUser.ExpiresIn
		}
	}
	if resp.StatusCode >= 400 || tr.Error != "" || tr.AccessToken == "" {
		return Credential{}, fmt.Errorf("token endpoint refused the request (HTTP %d)", resp.StatusCode)
	}
	c := Credential{Kind: "oauth", AccessToken: tr.AccessToken, RefreshToken: tr.RefreshToken, ClientID: p.ClientID, ClientSecret: p.ClientSecret}
	c.PublicClient = p.PKCE
	if tr.ExpiresIn > 0 {
		c.Expiry = time.Now().UTC().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return c, nil
}

// Exchange trades an authorization code for tokens.
func (p Provider) Exchange(ctx context.Context, client *http.Client, code, redirect string) (Credential, error) {
	return p.ExchangeWithVerifier(ctx, client, code, redirect, "")
}

func (p Provider) ExchangeWithVerifier(ctx context.Context, client *http.Client, code, redirect, verifier string) (Credential, error) {
	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect},
		"client_id": {p.ClientID}, "client_secret": {p.ClientSecret},
	}
	if p.PKCE {
		if verifier == "" {
			return Credential{}, errors.New("missing PKCE verifier")
		}
		form.Set("code_verifier", verifier)
	}
	return p.token(ctx, client, form)
}

// Refresh renews an expired access token, keeping the refresh token Google
// omits from the response.
func (p Provider) Refresh(ctx context.Context, client *http.Client, old Credential) (Credential, error) {
	if old.RefreshToken == "" {
		return Credential{}, errors.New("the connection expired and has no refresh token; reconnect it in the console")
	}
	p.ClientID, p.ClientSecret = old.ClientID, old.ClientSecret
	if p.SlackUser {
		p.PKCE = old.PublicClient
	}
	next, err := p.token(ctx, client, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {old.RefreshToken},
		"client_id": {old.ClientID}, "client_secret": {old.ClientSecret},
	})
	if err != nil {
		return Credential{}, err
	}
	if next.RefreshToken == "" {
		next.RefreshToken = old.RefreshToken
	}
	next.Account, next.ConnectedAt = old.Account, old.ConnectedAt
	return next, nil
}

func firstNonEmpty(items ...string) string {
	for _, s := range items {
		if s != "" {
			return s
		}
	}
	return ""
}
