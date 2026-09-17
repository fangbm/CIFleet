package githubapp

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const defaultAPIVersion = "2026-03-10"

type Config struct {
	Issuer     string
	PrivateKey *rsa.PrivateKey
	BaseURL    string
	APIVersion string
	HTTPClient *http.Client
	Now        func() time.Time
}

type Client struct {
	issuer     string
	privateKey *rsa.PrivateKey
	baseURL    string
	apiVersion string
	httpClient *http.Client
	now        func() time.Time

	tokenMu sync.Mutex
	tokens  map[int64]installationToken
}

type installationToken struct {
	Value     string
	ExpiresAt time.Time
}

type RepositoryRef struct {
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
}

type WorkflowRun struct {
	ID             int64          `json:"id"`
	Event          string         `json:"event"`
	Repository     RepositoryRef  `json:"repository"`
	HeadRepository *RepositoryRef `json:"head_repository"`
}

type JITConfig struct {
	RunnerID      int64
	EncodedConfig string
}

func LoadPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read GitHub App private key: %w", err)
	}
	return ParsePrivateKey(data)
}

func ParsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("GitHub App private key is not PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse GitHub App private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("GitHub App private key is not RSA")
	}
	return key, nil
}

func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Issuer) == "" {
		return nil, errors.New("GitHub App issuer is required")
	}
	if cfg.PrivateKey == nil {
		return nil, errors.New("GitHub App private key is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.github.com"
	}
	if _, err := url.ParseRequestURI(cfg.BaseURL); err != nil {
		return nil, fmt.Errorf("invalid GitHub API base URL: %w", err)
	}
	if cfg.APIVersion == "" {
		cfg.APIVersion = defaultAPIVersion
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Client{
		issuer:     cfg.Issuer,
		privateKey: cfg.PrivateKey,
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		apiVersion: cfg.APIVersion,
		httpClient: cfg.HTTPClient,
		now:        cfg.Now,
		tokens:     make(map[int64]installationToken),
	}, nil
}

func (c *Client) GetWorkflowRun(ctx context.Context, installationID int64, owner, repo string, runID int64) (WorkflowRun, error) {
	token, err := c.installationAccessToken(ctx, installationID)
	if err != nil {
		return WorkflowRun{}, err
	}
	var out WorkflowRun
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d", url.PathEscape(owner), url.PathEscape(repo), runID)
	if err := c.doJSON(ctx, http.MethodGet, path, token, nil, &out); err != nil {
		return WorkflowRun{}, err
	}
	return out, nil
}

func (c *Client) GenerateJITConfig(ctx context.Context, installationID int64, owner, repo, runnerName string, runnerGroupID int64, labels []string) (JITConfig, error) {
	if runnerGroupID <= 0 {
		return JITConfig{}, errors.New("runner group id must be positive")
	}
	if strings.TrimSpace(runnerName) == "" {
		return JITConfig{}, errors.New("runner name is required")
	}
	if len(labels) == 0 {
		return JITConfig{}, errors.New("at least one runner label is required")
	}
	token, err := c.installationAccessToken(ctx, installationID)
	if err != nil {
		return JITConfig{}, err
	}
	body := struct {
		Name          string   `json:"name"`
		RunnerGroupID int64    `json:"runner_group_id"`
		Labels        []string `json:"labels"`
		WorkFolder    string   `json:"work_folder,omitempty"`
	}{Name: runnerName, RunnerGroupID: runnerGroupID, Labels: labels, WorkFolder: "_work"}
	var response struct {
		Runner struct {
			ID int64 `json:"id"`
		} `json:"runner"`
		EncodedJITConfig string `json:"encoded_jit_config"`
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runners/generate-jitconfig", url.PathEscape(owner), url.PathEscape(repo))
	if err := c.doJSON(ctx, http.MethodPost, path, token, body, &response); err != nil {
		return JITConfig{}, err
	}
	if response.Runner.ID == 0 || response.EncodedJITConfig == "" {
		return JITConfig{}, errors.New("GitHub returned an incomplete JIT runner configuration")
	}
	return JITConfig{RunnerID: response.Runner.ID, EncodedConfig: response.EncodedJITConfig}, nil
}

func (c *Client) DeleteRunner(ctx context.Context, installationID int64, owner, repo string, runnerID int64) error {
	if runnerID <= 0 {
		return nil
	}
	token, err := c.installationAccessToken(ctx, installationID)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runners/%d", url.PathEscape(owner), url.PathEscape(repo), runnerID)
	return c.doJSON(ctx, http.MethodDelete, path, token, nil, nil)
}

func (c *Client) installationAccessToken(ctx context.Context, installationID int64) (string, error) {
	if installationID <= 0 {
		return "", errors.New("GitHub App installation id is required")
	}
	now := c.now().UTC()
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if cached, ok := c.tokens[installationID]; ok && cached.Value != "" && cached.ExpiresAt.After(now.Add(5*time.Minute)) {
		return cached.Value, nil
	}
	jwt, err := c.jwt(now)
	if err != nil {
		return "", err
	}
	var response struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	path := fmt.Sprintf("/app/installations/%d/access_tokens", installationID)
	if err := c.doJSON(ctx, http.MethodPost, path, jwt, struct{}{}, &response); err != nil {
		return "", err
	}
	if response.Token == "" || response.ExpiresAt.IsZero() {
		return "", errors.New("GitHub returned an incomplete installation token")
	}
	c.tokens[installationID] = installationToken{Value: response.Token, ExpiresAt: response.ExpiresAt}
	return response.Token, nil
}

func (c *Client) jwt(now time.Time) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": c.issuer,
	})
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	unsigned := enc.EncodeToString(header) + "." + enc.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign GitHub App JWT: %w", err)
	}
	return unsigned + "." + enc.EncodeToString(signature), nil
}

func (c *Client) doJSON(ctx context.Context, method, path, bearer string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode GitHub request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", c.apiVersion)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub API %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("GitHub API %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(message)))
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode GitHub API response: %w", err)
	}
	return nil
}
