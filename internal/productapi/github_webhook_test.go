package productapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/internal/organization"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
	"github.com/khangdoan514/hacktx-2026/internal/platform/testdb"
)

func TestGitHubSignature(t *testing.T) {
	// Fixture published by GitHub in its webhook validation documentation.
	const header = "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17"
	if !validGitHubSignature([]byte("It's a Secret to Everybody"), []byte("Hello, World!"), header) {
		t.Fatal("GitHub fixture signature rejected")
	}
	if validGitHubSignature([]byte("It's a Secret to Everybody"), []byte("Hello, world!"), header) {
		t.Fatal("tampered body accepted")
	}
}

func TestGitHubWebhookRequiresSignatureAndDeduplicates(t *testing.T) {
	db := testdb.DB(t)
	if err := organization.Seed(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_WEBHOOK_SECRET", "github-test-secret")
	projectID, deliveryID := platform.UUID(), platform.UUID()
	installationID, repositoryID := time.Now().UnixNano(), time.Now().UnixNano()+1
	if _, err := db.Pool.Exec(context.Background(), `INSERT INTO app.delivery_projects (id,organization_id,name) VALUES ($1,$2,'GitHub webhook test')`, projectID, platform.OrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(context.Background(), `INSERT INTO app.github_installations (installation_id,account_id,account_login) VALUES ($1,$2,'example')`, installationID, installationID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(context.Background(), `INSERT INTO app.project_github_repositories
		(project_id,organization_id,repository_id,full_name,installation_id,connected_by)
		VALUES ($1,$2,$3,'example/private',$4,$5)`, projectID, platform.OrgID, repositoryID, installationID, organization.BaoID); err != nil {
		t.Fatal(err)
	}
	cfg := platform.Config{Environment: "test", AllowLocalAuth: true, AgentExecutionMode: "dummy", JWTSecret: "test-secret-test-secret-test-secret", JWTIssuer: "http://127.0.0.1/test-auth", JWTAudience: "authenticated", StorageDir: t.TempDir()}
	server, _, err := Wire(context.Background(), cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	app := httptest.NewServer(server.Handler())
	defer app.Close()
	payload, _ := json.Marshal(map[string]any{"action": "opened", "repository": map[string]int64{"id": repositoryID}, "installation": map[string]int64{"id": installationID}})
	send := func(id, signature string) (int, map[string]bool) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, app.URL+"/api/v1/github/webhooks", bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-GitHub-Delivery", id)
		req.Header.Set("X-GitHub-Event", "issues")
		req.Header.Set("X-Hub-Signature-256", signature)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var result map[string]bool
		if res.StatusCode == http.StatusAccepted {
			if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
		}
		return res.StatusCode, result
	}
	mac := hmac.New(sha256.New, []byte("github-test-secret"))
	_, _ = mac.Write(payload)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if status, _ := send(deliveryID, "sha256=bad"); status != http.StatusUnauthorized {
		t.Fatalf("bad signature status %d", status)
	}
	if status, result := send(deliveryID, signature); status != http.StatusAccepted || !result["queued"] || result["duplicate"] {
		t.Fatalf("first delivery: %d %#v", status, result)
	}
	if status, result := send(deliveryID, signature); status != http.StatusAccepted || !result["duplicate"] {
		t.Fatalf("duplicate delivery: %d %#v", status, result)
	}
	var count int
	if err := db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM app.github_webhook_deliveries WHERE delivery_id=$1 AND state='queued'`, deliveryID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("recorded deliveries=%d err=%v", count, err)
	}
	lifecycle := func(event string, body map[string]any) {
		t.Helper()
		data, _ := json.Marshal(body)
		mac := hmac.New(sha256.New, []byte("github-test-secret"))
		_, _ = mac.Write(data)
		req, _ := http.NewRequest(http.MethodPost, app.URL+"/api/v1/github/webhooks", bytes.NewReader(data))
		req.Header.Set("X-GitHub-Event", event)
		req.Header.Set("X-GitHub-Delivery", platform.UUID())
		req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusAccepted {
			t.Fatalf("%s status=%d", event, res.StatusCode)
		}
	}
	lifecycle("installation_repositories", map[string]any{"action": "removed", "installation": map[string]int64{"id": installationID},
		"repositories_removed": []map[string]int64{{"id": repositoryID}}})
	var enabled bool
	if err := db.Pool.QueryRow(context.Background(), `SELECT enabled FROM app.project_github_repositories WHERE project_id=$1 AND repository_id=$2`, projectID, repositoryID).Scan(&enabled); err != nil || enabled {
		t.Fatalf("removed repo enabled=%v err=%v", enabled, err)
	}
	lifecycle("installation", map[string]any{"action": "suspend", "installation": map[string]int64{"id": installationID}})
	var installStatus string
	if err := db.Pool.QueryRow(context.Background(), `SELECT status FROM app.github_installations WHERE installation_id=$1`, installationID).Scan(&installStatus); err != nil || installStatus != "suspended" {
		t.Fatalf("installation status=%s err=%v", installStatus, err)
	}
}
