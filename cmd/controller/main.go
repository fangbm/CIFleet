package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fangbm/cifleet/internal/controller"
	"github.com/fangbm/cifleet/internal/githubapp"
	"github.com/fangbm/cifleet/internal/tlsutil"
)

func main() {
	listen := flag.String("listen", env("CIFLEET_LISTEN", ":8080"), "controller mTLS listen address")
	caFile := flag.String("ca-file", env("CIFLEET_CA_FILE", "/etc/cifleet/pki/ca.crt"), "trusted client CA")
	certFile := flag.String("cert-file", env("CIFLEET_CERT_FILE", "/etc/cifleet/pki/controller.crt"), "controller certificate")
	keyFile := flag.String("key-file", env("CIFLEET_KEY_FILE", "/etc/cifleet/pki/controller.key"), "controller private key")
	nodeTTL := flag.Duration("node-ttl", envDuration("CIFLEET_NODE_TTL", 45*time.Second), "mark a node offline after this interval")
	insecureHTTP := flag.Bool("insecure-http", false, "serve the control plane as plaintext HTTP (development only)")

	webhookListen := flag.String("webhook-listen", env("CIFLEET_WEBHOOK_LISTEN", "127.0.0.1:8081"), "public GitHub webhook listen address behind an HTTPS reverse proxy")
	githubIssuer := flag.String("github-app-issuer", env("CIFLEET_GITHUB_APP_ISSUER", ""), "GitHub App client ID or app ID; empty disables M2")
	githubPrivateKey := flag.String("github-app-private-key", env("CIFLEET_GITHUB_APP_PRIVATE_KEY_FILE", "/etc/cifleet/github/app.pem"), "GitHub App private key")
	githubWebhookSecret := flag.String("github-webhook-secret-file", env("CIFLEET_GITHUB_WEBHOOK_SECRET_FILE", "/etc/cifleet/github/webhook-secret"), "file containing GitHub webhook secret")
	githubAPIURL := flag.String("github-api-url", env("CIFLEET_GITHUB_API_URL", "https://api.github.com"), "GitHub REST API base URL")
	githubAPIVersion := flag.String("github-api-version", env("CIFLEET_GITHUB_API_VERSION", "2026-03-10"), "GitHub REST API version")
	markerLabel := flag.String("github-marker-label", env("CIFLEET_GITHUB_MARKER_LABEL", "cifleet"), "workflow label that opts a job into CIFleet")
	runnerGroupID := flag.Int64("runner-group-id", envInt64("CIFLEET_RUNNER_GROUP_ID", 1), "GitHub runner group id")
	runnerImage := flag.String("runner-image", env("CIFLEET_RUNNER_IMAGE", "ghcr.io/actions/actions-runner:latest"), "Actions runner container image")
	runnerCPU := flag.Int("runner-cpu", envInt("CIFLEET_RUNNER_CPU", 2), "default CPU reservation per runner")
	runnerMemory := flag.Int("runner-memory-mb", envInt("CIFLEET_RUNNER_MEMORY_MB", 4096), "default memory reservation per runner in MiB")
	runnerTimeout := flag.Duration("runner-timeout", envDuration("CIFLEET_RUNNER_TIMEOUT", 2*time.Hour), "maximum lifetime of a JIT runner container")
	stateFile := flag.String("state-file", env("CIFLEET_STATE_FILE", "/var/lib/cifleet/controller-state.json"), "persistent controller job state")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	control := controller.New(log, *nodeTTL)
	controlServer := &http.Server{Addr: *listen, Handler: control.Handler(), ReadHeaderTimeout: 5 * time.Second}
	agentHTTPClient := &http.Client{Timeout: 5 * time.Minute}

	if *insecureHTTP {
		log.Warn("controller control plane starting with insecure HTTP", "listen", *listen)
	} else {
		serverTLS, err := tlsutil.LoadServerConfig(*caFile, *certFile, *keyFile)
		if err != nil {
			fatal(log, "load control-plane mTLS server configuration", err)
		}
		controlServer.TLSConfig = serverTLS
		clientTLS, err := tlsutil.LoadClientConfig(*caFile, *certFile, *keyFile, "")
		if err != nil {
			fatal(log, "load worker mTLS client configuration", err)
		}
		agentHTTPClient.Transport = &http.Transport{TLSClientConfig: clientTLS}
	}

	var webhookServer *http.Server
	if strings.TrimSpace(*githubIssuer) != "" {
		privateKey, err := githubapp.LoadPrivateKey(*githubPrivateKey)
		if err != nil {
			fatal(log, "load GitHub App private key", err)
		}
		githubClient, err := githubapp.New(githubapp.Config{Issuer: *githubIssuer, PrivateKey: privateKey, BaseURL: *githubAPIURL, APIVersion: *githubAPIVersion})
		if err != nil {
			fatal(log, "initialize GitHub App client", err)
		}
		secret, err := readSecret(*githubWebhookSecret)
		if err != nil {
			fatal(log, "read GitHub webhook secret", err)
		}
		agentClient := controller.NewHTTPAgentClient(agentHTTPClient, *insecureHTTP)
		manager, err := controller.NewRunnerManager(controller.RunnerManagerConfig{MarkerLabel: *markerLabel, RunnerGroupID: *runnerGroupID, RunnerImage: *runnerImage, RunnerCPU: *runnerCPU, RunnerMemoryM: *runnerMemory, RunnerTimeout: *runnerTimeout, StateFile: *stateFile}, control, githubClient, agentClient, log)
		if err != nil {
			fatal(log, "initialize runner manager", err)
		}
		webhook, err := controller.NewWebhookServer(secret, *markerLabel, manager, log)
		if err != nil {
			fatal(log, "initialize GitHub webhook server", err)
		}
		webhookServer = &http.Server{Addr: *webhookListen, Handler: webhook.Handler(), ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := manager.Run(ctx); err != nil {
				log.Error("runner manager stopped", "error", err)
				stop()
			}
		}()
		go func() {
			log.Info("GitHub webhook ingress starting", "listen", *webhookListen, "path", "/github/webhook", "marker_label", *markerLabel)
			if err := webhookServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error("GitHub webhook server stopped", "error", err)
				stop()
			}
		}()
	} else {
		log.Warn("GitHub App issuer is not configured; M2 webhook/JIT integration is disabled")
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = controlServer.Shutdown(shutdownCtx)
		if webhookServer != nil {
			_ = webhookServer.Shutdown(shutdownCtx)
		}
	}()

	log.Info("controller control plane starting", "listen", *listen, "node_ttl", nodeTTL.String(), "mtls", !*insecureHTTP)
	var err error
	if *insecureHTTP {
		err = controlServer.ListenAndServe()
	} else {
		err = controlServer.ListenAndServeTLS("", "")
	}
	if err != nil && err != http.ErrServerClosed {
		fatal(log, "controller stopped", err)
	}
}

func readSecret(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return nil, fmt.Errorf("secret file %s is empty", path)
	}
	return []byte(secret), nil
}
func fatal(log *slog.Logger, message string, err error) { log.Error(message, "error", err); os.Exit(1) }
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func envDuration(key string, fallback time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return fallback
}
func envInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return fallback
}
func envInt64(key string, fallback int64) int64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
	}
	return fallback
}
