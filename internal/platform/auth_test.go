package platform

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestSupabaseStyleJWTUsesJWKS(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks := map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "kid": "test-key", "alg": "RS256",
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()
	cfg := Config{
		Environment: "production", AgentExecutionMode: "dummy",
		JWTIssuer: "https://project.supabase.co/auth/v1", JWTAudience: "authenticated", JWKSURL: server.URL,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	claims := SessionClaims{Email: "ada.hardware@nmc2.test", RegisteredClaims: jwt.RegisteredClaims{
		Subject: AdaPlaceholder(), Issuer: cfg.JWTIssuer, Audience: jwt.ClaimStrings{cfg.JWTAudience},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key"
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := Verify(cfg, raw, NewJWKS(cfg.JWKSURL))
	if err != nil {
		t.Fatal(err)
	}
	if identity.Email != "ada.hardware@nmc2.test" {
		t.Fatalf("identity %#v", identity)
	}
	local := cfg
	local.Environment = "production"
	hs, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(DevJWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(local, hs, NewJWKS(cfg.JWKSURL)); err == nil {
		t.Fatal("production accepted the development HMAC secret")
	}
}

func AdaPlaceholder() string { return "20000000-0000-4000-8000-000000000001" }
