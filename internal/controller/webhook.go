package controller

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

const maxWebhookBody = 2 << 20

type EventSubmitter interface {
	Submit(event WorkflowJobEvent) error
}

type WebhookServer struct {
	secret []byte
	marker string
	submit EventSubmitter
	log    *slog.Logger
}

func NewWebhookServer(secret []byte, marker string, submit EventSubmitter, log *slog.Logger) (*WebhookServer, error) {
	if len(secret) == 0 {
		return nil, errors.New("GitHub webhook secret is required")
	}
	if submit == nil {
		return nil, errors.New("GitHub event submitter is required")
	}
	if strings.TrimSpace(marker) == "" {
		marker = "cifleet"
	}
	if log == nil {
		log = slog.Default()
	}
	return &WebhookServer{secret: append([]byte(nil), secret...), marker: marker, submit: submit, log: log}, nil
}

func (s *WebhookServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, map[string]any{"ok": true}) })
	mux.HandleFunc("POST /github/webhook", s.webhook)
	return mux
}

func (s *WebhookServer) webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read webhook body"})
		return
	}
	if len(body) > maxWebhookBody {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "webhook body too large"})
		return
	}
	if !validWebhookSignature(s.secret, body, r.Header.Get("X-Hub-Signature-256")) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid webhook signature"})
		return
	}
	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "ping" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if eventType != "workflow_job" {
		writeJSON(w, http.StatusAccepted, map[string]any{"ignored": true})
		return
	}
	var payload struct {
		Action       string `json:"action"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Repository struct {
			FullName string `json:"full_name"`
			Name     string `json:"name"`
			Private  bool   `json:"private"`
			Owner    struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repository"`
		WorkflowJob struct {
			ID         int64    `json:"id"`
			RunID      int64    `json:"run_id"`
			Name       string   `json:"name"`
			Labels     []string `json:"labels"`
			RunnerName string   `json:"runner_name"`
		} `json:"workflow_job"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid workflow_job payload"})
		return
	}
	event := WorkflowJobEvent{DeliveryID: r.Header.Get("X-GitHub-Delivery"), Action: payload.Action, InstallationID: payload.Installation.ID, Repository: RepositoryInfo{FullName: payload.Repository.FullName, Owner: payload.Repository.Owner.Login, Name: payload.Repository.Name, Private: payload.Repository.Private}, Job: WorkflowJobInfo{ID: payload.WorkflowJob.ID, RunID: payload.WorkflowJob.RunID, Name: payload.WorkflowJob.Name, Labels: append([]string(nil), payload.WorkflowJob.Labels...), RunnerName: payload.WorkflowJob.RunnerName}}
	if event.Action == "queued" && !hasLabel(event.Job.Labels, s.marker) {
		writeJSON(w, http.StatusAccepted, map[string]any{"ignored": true})
		return
	}
	if event.Action == "completed" && !hasLabel(event.Job.Labels, s.marker) && !strings.HasPrefix(strings.ToLower(event.Job.RunnerName), "cifleet-") {
		writeJSON(w, http.StatusAccepted, map[string]any{"ignored": true})
		return
	}
	if event.Action != "queued" && event.Action != "completed" {
		writeJSON(w, http.StatusAccepted, map[string]any{"ignored": true})
		return
	}
	if err := s.submit.Submit(event); err != nil {
		s.log.Warn("GitHub event queue rejected delivery", "delivery_id", event.DeliveryID, "job_id", event.Job.ID, "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true})
}

func validWebhookSignature(secret, body []byte, header string) bool {
	if len(secret) == 0 || !strings.HasPrefix(header, "sha256=") {
		return false
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	return hmac.Equal(mac.Sum(nil), provided)
}
