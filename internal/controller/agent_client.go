package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/fangbm/cifleet/internal/backend"
	"github.com/fangbm/cifleet/internal/model"
)

type AgentClient interface {
	CreateInstance(ctx context.Context, node model.Node, spec backend.JobSpec) (*backend.Instance, error)
	DestroyInstance(ctx context.Context, node model.Node, instanceID string) error
}

type HTTPAgentClient struct {
	client    *http.Client
	allowHTTP bool
}

func NewHTTPAgentClient(client *http.Client, allowHTTP bool) *HTTPAgentClient {
	return &HTTPAgentClient{client: client, allowHTTP: allowHTTP}
}

func (c *HTTPAgentClient) CreateInstance(ctx context.Context, node model.Node, spec backend.JobSpec) (*backend.Instance, error) {
	endpoint, err := c.endpoint(node)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("encode agent job spec: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/instances", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agent %s create instance: %w", node.ID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("agent %s create instance returned %d: %s", node.ID, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var instance backend.Instance
	if err := json.NewDecoder(resp.Body).Decode(&instance); err != nil {
		return nil, fmt.Errorf("decode agent instance: %w", err)
	}
	if instance.ID == "" {
		return nil, fmt.Errorf("agent %s returned empty instance id", node.ID)
	}
	return &instance, nil
}

func (c *HTTPAgentClient) DestroyInstance(ctx context.Context, node model.Node, instanceID string) error {
	endpoint, err := c.endpoint(node)
	if err != nil {
		return err
	}
	if strings.TrimSpace(instanceID) == "" {
		return fmt.Errorf("instance id is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint+"/v1/instances/"+url.PathEscape(instanceID), nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("agent %s destroy instance: %w", node.ID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("agent %s destroy instance returned %d: %s", node.ID, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *HTTPAgentClient) endpoint(node model.Node) (string, error) {
	if c.client == nil {
		return "", fmt.Errorf("agent HTTP client is required")
	}
	raw := strings.TrimRight(strings.TrimSpace(node.Endpoint), "/")
	if raw == "" {
		return "", fmt.Errorf("node %s has no endpoint", node.ID)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("node %s has invalid endpoint %q", node.ID, node.Endpoint)
	}
	if parsed.Scheme != "https" && !(c.allowHTTP && parsed.Scheme == "http") {
		return "", fmt.Errorf("node %s endpoint must use https", node.ID)
	}
	return raw, nil
}
