package platform

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

const (
	DevJWTSecret   = "dev-only-change-me"
	AdapterVersion = "dummy-1"
	OrgID          = "10000000-0000-4000-8000-000000000001"
	HardwareTeamID = "10000000-0000-4000-8000-000000000004"
	AITeamID       = "10000000-0000-4000-8000-000000000005"
	ReviewTeamID   = "10000000-0000-4000-8000-000000000006"
	// SupabaseAuthIssuer is the hosted Auth issuer for this project.
	// Access tokens are RS256 or ES256 and carry the employee email.
	SupabaseAuthIssuer = "https://vocvwmiypifqnncxhfpj.supabase.co/auth/v1"
)

type Config struct {
	Environment        string
	DatabaseURL        string
	JWTSecret          string
	JWTIssuer          string
	JWTAudience        string
	JWKSURL            string
	AllowLocalAuth     bool
	AgentExecutionMode string
	StorageDir         string
	Addr               string
	AgentAPIURL        string
}

func Load() Config {
	issuer := env("JWT_ISSUER", SupabaseAuthIssuer)
	jwks := os.Getenv("JWT_JWKS_URL")
	if strings.TrimSpace(jwks) == "" && issuer == SupabaseAuthIssuer {
		jwks = SupabaseAuthIssuer + "/.well-known/jwks.json"
	}
	return Config{
		Environment:        env("ENVIRONMENT", "production"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		JWTSecret:          os.Getenv("JWT_SECRET"),
		JWTIssuer:          issuer,
		JWTAudience:        env("JWT_AUDIENCE", "authenticated"),
		JWKSURL:            jwks,
		AllowLocalAuth:     os.Getenv("ALLOW_LOCAL_AUTH") == "true",
		AgentExecutionMode: env("AGENT_EXECUTION_MODE", "dummy"),
		StorageDir:         env("STORAGE_DIR", "var/storage"),
		Addr:               env("ADDR", ":8000"),
		AgentAPIURL:        env("AGENT_API_URL", "http://127.0.0.1:8080"),
	}
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func (c Config) Validate() error {
	name := c.Environment
	if name == "" {
		name = "production"
	}
	local := name == "local" || name == "test"
	if c.AllowLocalAuth && !local {
		return fmt.Errorf("ALLOW_LOCAL_AUTH requires ENVIRONMENT=local or ENVIRONMENT=test")
	}
	if c.AgentExecutionMode != "dummy" {
		return fmt.Errorf("AGENT_EXECUTION_MODE=%s is not available; this process does not call a provider or fall back to dummy results", c.AgentExecutionMode)
	}
	if local {
		return nil
	}
	if c.JWTSecret == DevJWTSecret {
		return fmt.Errorf("production rejects the development JWT secret")
	}
	if strings.TrimSpace(c.JWTIssuer) == "" || strings.TrimSpace(c.JWTAudience) == "" || strings.TrimSpace(c.JWKSURL) == "" {
		return fmt.Errorf("production requires a JWT issuer, audience, and JWKS URL")
	}
	if devIssuer(c.JWTIssuer) {
		return fmt.Errorf("production rejects the development JWT issuer")
	}
	return nil
}

func devIssuer(issuer string) bool {
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Hostname() == "" {
		return true
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".local")
}
