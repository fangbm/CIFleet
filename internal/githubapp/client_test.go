package githubapp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestJWTClaimsAndSignature(t *testing.T) {
	key := testKey(t)
	now := time.Unix(1_800_000_000, 0).UTC()
	client, err := New(Config{Issuer: "Iv1.client-id", PrivateKey: key, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	token, err := client.jwt(now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected JWT with 3 parts, got %d", len(parts))
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["iss"] != "Iv1.client-id" {
		t.Fatalf("unexpected issuer: %#v", claims["iss"])
	}
	if got := int64(claims["iat"].(float64)); got != now.Add(-time.Minute).Unix() {
		t.Fatalf("unexpected iat: %d", got)
	}
	if got := int64(claims["exp"].(float64)); got != now.Add(9*time.Minute).Unix() {
		t.Fatalf("unexpected exp: %d", got)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("JWT signature invalid: %v", err)
	}
}

func TestInstallationTokenIsCachedAndJITConfigGenerated(t *testing.T) {
	key := testKey(t)
	now := time.Unix(1_800_000_000, 0).UTC()
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-GitHub-Api-Version") != defaultAPIVersion {
			t.Errorf("missing API version header")
		}
		switch r.URL.Path {
		case "/app/installations/42/access_tokens":
			tokenCalls.Add(1)
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				t.Errorf("missing app JWT")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_test", "expires_at": now.Add(time.Hour)})
		case "/repos/fangbm/CIFleet/actions/runs/100":
			if r.Header.Get("Authorization") != "Bearer ghs_test" {
				t.Errorf("wrong installation token")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 100, "event": "push", "repository": map[string]any{"full_name": "fangbm/CIFleet", "private": false}, "head_repository": map[string]any{"full_name": "fangbm/CIFleet", "private": false}})
		case "/repos/fangbm/CIFleet/actions/runners/generate-jitconfig":
			var body struct {
				Name          string   `json:"name"`
				RunnerGroupID int64    `json:"runner_group_id"`
				Labels        []string `json:"labels"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			if body.Name != "cifleet-e5-200" || body.RunnerGroupID != 1 || len(body.Labels) != 3 {
				t.Errorf("unexpected JIT body: %+v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"runner": map[string]any{"id": 77}, "encoded_jit_config": "abc123"})
		case "/repos/fangbm/CIFleet/actions/runners/77":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(Config{Issuer: "Iv1.test", PrivateKey: key, BaseURL: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	run, err := client.GetWorkflowRun(ctx, 42, "fangbm", "CIFleet", 100)
	if err != nil {
		t.Fatal(err)
	}
	if run.Event != "push" || run.Repository.FullName != "fangbm/CIFleet" {
		t.Fatalf("unexpected run: %+v", run)
	}
	jit, err := client.GenerateJITConfig(ctx, 42, "fangbm", "CIFleet", "cifleet-e5-200", 1, []string{"self-hosted", "cifleet", "linux-x64"})
	if err != nil {
		t.Fatal(err)
	}
	if jit.RunnerID != 77 || jit.EncodedConfig != "abc123" {
		t.Fatalf("unexpected JIT config: %+v", jit)
	}
	if err := client.DeleteRunner(ctx, 42, "fangbm", "CIFleet", 77); err != nil {
		t.Fatal(err)
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("expected installation token to be cached, calls=%d", got)
	}
}
