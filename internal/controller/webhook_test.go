package controller

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type captureSubmitter struct {
	events []WorkflowJobEvent
	err    error
}

func (c *captureSubmitter) Submit(event WorkflowJobEvent) error {
	c.events = append(c.events, event)
	return c.err
}

func signWebhook(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookSignatureGitHubVector(t *testing.T) {
	secret := []byte("It's a Secret to Everybody")
	body := []byte("Hello, World!")
	if !validWebhookSignature(secret, body, "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17") {
		t.Fatal("GitHub reference signature did not validate")
	}
}

func TestWebhookAcceptsCIFleetQueuedJob(t *testing.T) {
	submit := &captureSubmitter{}
	server, err := NewWebhookServer([]byte("secret"), "cifleet", submit, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"action":"queued","installation":{"id":42},"repository":{"full_name":"fangbm/CIFleet","name":"CIFleet","private":false,"owner":{"login":"fangbm"}},"workflow_job":{"id":12,"run_id":10,"name":"smoke","labels":["self-hosted","cifleet","linux-x64"],"runner_name":""}}`)
	req := httptest.NewRequest(http.MethodPost, "/github/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	req.Header.Set("X-Hub-Signature-256", signWebhook("secret", body))
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(submit.events) != 1 || submit.events[0].Job.ID != 12 || submit.events[0].InstallationID != 42 {
		t.Fatalf("unexpected events: %+v", submit.events)
	}
}

func TestWebhookRejectsInvalidSignature(t *testing.T) {
	submit := &captureSubmitter{}
	server, _ := NewWebhookServer([]byte("secret"), "cifleet", submit, slog.Default())
	req := httptest.NewRequest(http.MethodPost, "/github/webhook", strings.NewReader(`{"action":"queued"}`))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	req.Header.Set("X-Hub-Signature-256", "sha256=00")
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if len(submit.events) != 0 {
		t.Fatal("invalid webhook was submitted")
	}
}

func TestWebhookIgnoresNonCIFleetJob(t *testing.T) {
	submit := &captureSubmitter{}
	server, _ := NewWebhookServer([]byte("secret"), "cifleet", submit, slog.Default())
	body := []byte(`{"action":"queued","installation":{"id":42},"repository":{"full_name":"fangbm/CIFleet","name":"CIFleet","owner":{"login":"fangbm"}},"workflow_job":{"id":12,"run_id":10,"labels":["ubuntu-latest"]}}`)
	req := httptest.NewRequest(http.MethodPost, "/github/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	req.Header.Set("X-Hub-Signature-256", signWebhook("secret", body))
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
	if len(submit.events) != 0 {
		t.Fatal("non-CIFleet job should be ignored")
	}
}
