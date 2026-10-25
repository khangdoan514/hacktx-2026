package productapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestGitHubAppVerifiesInstallationAndRepositoryScope(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	var requestedRepositoryIDs []int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/app/installations/123":
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			claims := &jwt.RegisteredClaims{}
			if _, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return &privateKey.PublicKey, nil }); err != nil || claims.Issuer != "test-app" {
				t.Errorf("invalid app JWT: %v issuer=%q", err, claims.Issuer)
			}
			_, _ = w.Write([]byte(`{"id":123,"account":{"id":456,"login":"example"},"suspended_at":null}`))
		case "/app/installations/123/access_tokens":
			var body struct {
				RepositoryIDs []int64           `json:"repository_ids"`
				Permissions   map[string]string `json:"permissions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			requestedRepositoryIDs = body.RepositoryIDs
			if body.Permissions["contents"] != "read" || body.Permissions["issues"] != "read" || body.Permissions["pull_requests"] != "read" {
				t.Errorf("permissions=%v", body.Permissions)
			}
			_, _ = w.Write([]byte(`{"token":"scoped-installation-token"}`))
		case "/repos/example/private":
			if r.Header.Get("Authorization") != "Bearer scoped-installation-token" {
				t.Error("repository read did not use installation token")
			}
			_, _ = w.Write([]byte(`{"id":789,"full_name":"example/private"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	app := &githubApp{Client: upstream.Client(), APIURL: upstream.URL, Issuer: "test-app", KeyPEM: keyPEM}
	verified, err := app.verifyRepository(context.Background(), 123, 789, "example/private")
	if err != nil {
		t.Fatal(err)
	}
	if verified.RepositoryID != 789 || verified.InstallationID != 123 || verified.AccountID != 456 || len(requestedRepositoryIDs) != 1 || requestedRepositoryIDs[0] != 789 {
		t.Fatalf("verified=%+v scoped repositories=%v", verified, requestedRepositoryIDs)
	}
	if _, err := app.verifyRepository(context.Background(), 123, 999, "example/private"); err == nil {
		t.Fatal("mismatched repository ID accepted")
	}
}
