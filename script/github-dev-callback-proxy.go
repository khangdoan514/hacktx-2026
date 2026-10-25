// Run with: go run ./script/github-dev-callback-proxy.go
// A temporary tunnel may point here during GitHub App development. This proxy
// exposes only the OAuth callback and signed webhook endpoint, never local
// demo sign-in, the application API, or the agent service.
package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
)

func main() {
	target, err := url.Parse("http://127.0.0.1:3012")
	if err != nil { log.Fatal(err) }
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		log.Printf("GitHub development proxy: %v", err)
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}
	addr := os.Getenv("GITHUB_DEV_PROXY_ADDR")
	if addr == "" { addr = "127.0.0.1:8765" }
	log.Printf("GitHub development callback proxy on %s", addr)
	log.Fatal(http.ListenAndServe(addr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed := r.Method == http.MethodGet && r.URL.Path == "/api/v1/github/oauth/callback" ||
			r.Method == http.MethodPost && r.URL.Path == "/api/v1/github/webhooks"
		if !allowed { http.NotFound(w, r); return }
		proxy.ServeHTTP(w, r)
	})))
}
