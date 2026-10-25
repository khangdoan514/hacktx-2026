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
	}
	return Provider{}, false
}

func (p Provider) AuthorizeURL(redirect, state string) string {
	q := url.Values{"client_id": {p.ClientID}, "redirect_uri": {redirect}, "scope": {p.Scope}, "state": {state}}
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
}

func (p Provider) token(ctx context.Context, client *http.Client, form url.Values) (Credential, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Credential{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
	if tr.Error != "" || tr.AccessToken == "" {
		return Credential{}, fmt.Errorf("token endpoint refused the request: %s", firstNonEmpty(tr.ErrorDesc, tr.Error, "no access token"))
	}
	c := Credential{Kind: "oauth", AccessToken: tr.AccessToken, RefreshToken: tr.RefreshToken, ClientID: p.ClientID, ClientSecret: p.ClientSecret}
	if tr.ExpiresIn > 0 {
		c.Expiry = time.Now().UTC().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return c, nil
}

// Exchange trades an authorization code for tokens.
func (p Provider) Exchange(ctx context.Context, client *http.Client, code, redirect string) (Credential, error) {
	return p.token(ctx, client, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect},
		"client_id": {p.ClientID}, "client_secret": {p.ClientSecret},
	})
}

// Refresh renews an expired access token, keeping the refresh token Google
// omits from the response.
func (p Provider) Refresh(ctx context.Context, client *http.Client, old Credential) (Credential, error) {
	if old.RefreshToken == "" {
		return Credential{}, errors.New("the connection expired and has no refresh token; reconnect it in the console")
	}
	p.ClientID, p.ClientSecret = old.ClientID, old.ClientSecret
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
