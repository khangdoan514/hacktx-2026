package platform

import "testing"

func TestProductionRejectsUnsafeJWTConfig(t *testing.T) {
	cases := []Config{
		{Environment: "production", AgentExecutionMode: "dummy"},
		{Environment: "production", AgentExecutionMode: "dummy", JWTSecret: DevJWTSecret, JWTIssuer: "https://project.supabase.co/auth/v1", JWTAudience: "authenticated", JWKSURL: "https://project.supabase.co/auth/v1/.well-known/jwks.json"},
		{Environment: "production", AgentExecutionMode: "dummy", JWTIssuer: "http://127.0.0.1/auth/v1", JWTAudience: "authenticated", JWKSURL: "https://project.supabase.co/auth/v1/.well-known/jwks.json"},
		{Environment: "production", AgentExecutionMode: "dummy", AllowLocalAuth: true, JWTIssuer: "https://project.supabase.co/auth/v1", JWTAudience: "authenticated", JWKSURL: "https://project.supabase.co/auth/v1/.well-known/jwks.json"},
		{Environment: "production", AgentExecutionMode: "provider", JWTIssuer: "https://project.supabase.co/auth/v1", JWTAudience: "authenticated", JWKSURL: "https://project.supabase.co/auth/v1/.well-known/jwks.json"},
	}
	for i, cfg := range cases {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("case %d accepted unsafe production config", i)
		}
	}
	ok := Config{
		Environment:        "production",
		AgentExecutionMode: "dummy",
		JWTIssuer:          "https://project.supabase.co/auth/v1",
		JWTAudience:        "authenticated",
		JWKSURL:            "https://project.supabase.co/auth/v1/.well-known/jwks.json",
	}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	local := Config{Environment: "local", AllowLocalAuth: true, AgentExecutionMode: "dummy", JWTSecret: DevJWTSecret}
	if err := local.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Config{Environment: "test", AllowLocalAuth: false, AgentExecutionMode: "dummy"}).Validate(); err != nil {
		t.Fatal(err)
	}
}
