package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fangbm/cifleet/internal/agent"
	dockerbackend "github.com/fangbm/cifleet/internal/backend/docker"
	"github.com/fangbm/cifleet/internal/tlsutil"
)

func main() {
	listen := flag.String("listen", env("CIFLEET_LISTEN", ":8090"), "agent listen address")
	nodeID := flag.String("node-id", env("CIFLEET_NODE_ID", "node"), "unique node id; must match client certificate CN")
	endpoint := flag.String("endpoint", env("CIFLEET_ENDPOINT", ""), "controller-reachable agent URL")
	controllerURL := flag.String("controller-url", env("CIFLEET_CONTROLLER_URL", ""), "controller base URL")
	caFile := flag.String("ca-file", env("CIFLEET_CA_FILE", "/etc/cifleet/pki/ca.crt"), "trusted CA")
	certFile := flag.String("cert-file", env("CIFLEET_CERT_FILE", "/etc/cifleet/pki/agent.crt"), "agent certificate")
	keyFile := flag.String("key-file", env("CIFLEET_KEY_FILE", "/etc/cifleet/pki/agent.key"), "agent private key")
	serverName := flag.String("controller-server-name", env("CIFLEET_CONTROLLER_SERVER_NAME", ""), "TLS server name override for the controller")
	controllerIdentity := flag.String("controller-identity", env("CIFLEET_CONTROLLER_IDENTITY", "controller"), "expected controller client certificate common name")
	dockerBin := flag.String("docker-bin", env("CIFLEET_DOCKER_BIN", "docker"), "Docker CLI path")
	cacheRoot := flag.String("cache-root", env("CIFLEET_CACHE_ROOT", "/var/lib/cifleet/cache"), "repository-scoped cache root")
	heartbeatInterval := flag.Duration("heartbeat-interval", envDuration("CIFLEET_HEARTBEAT_INTERVAL", 15*time.Second), "heartbeat interval")
	cleanupInterval := flag.Duration("cleanup-interval", envDuration("CIFLEET_CLEANUP_INTERVAL", time.Minute), "expired job cleanup interval")
	jobTimeout := flag.Duration("job-timeout", envDuration("CIFLEET_JOB_TIMEOUT", 2*time.Hour), "default Docker job timeout")
	capabilities := flag.String("capabilities", env("CIFLEET_CAPABILITIES", "container,docker"), "comma-separated node capabilities")
	labels := flag.String("labels", env("CIFLEET_LABELS", ""), "comma-separated node labels")
	insecureHTTP := flag.Bool("insecure-http", false, "disable mTLS for local development")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	backend := dockerbackend.New(dockerbackend.Config{Binary: *dockerBin, NodeID: *nodeID, CacheRoot: *cacheRoot, DefaultTimeout: *jobTimeout})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := &http.Client{Timeout: 10 * time.Second}
	var serverTLS *tls.Config
	if !*insecureHTTP {
		clientTLS, err := tlsutil.LoadClientConfig(*caFile, *certFile, *keyFile, *serverName)
		if err != nil {
			log.Error("load controller mTLS configuration", "error", err)
			os.Exit(1)
		}
		client.Transport = &http.Transport{TLSClientConfig: clientTLS}
		var errServer error
		serverTLS, errServer = tlsutil.LoadServerConfig(*caFile, *certFile, *keyFile)
		if errServer != nil {
			log.Error("load agent mTLS configuration", "error", errServer)
			os.Exit(1)
		}
	}

	if *controllerURL != "" {
		svc := agent.NewService(agent.ServiceConfig{NodeID: *nodeID, Endpoint: *endpoint, ControllerURL: strings.TrimRight(*controllerURL, "/"), HeartbeatInterval: *heartbeatInterval, CleanupInterval: *cleanupInterval, Capabilities: splitCSV(*capabilities), Labels: splitCSV(*labels)}, backend, client, log)
		go func() {
			if err := svc.Run(ctx); err != nil {
				log.Error("agent service stopped", "error", err)
				stop()
			}
		}()
	} else {
		log.Warn("controller URL not set; heartbeats disabled")
	}

	httpServer := &http.Server{Addr: *listen, Handler: (&agent.Server{NodeID: *nodeID, ControllerIdentity: *controllerIdentity, Backend: backend}).Handler(), ReadHeaderTimeout: 5 * time.Second, TLSConfig: serverTLS}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	if *insecureHTTP {
		log.Warn("agent starting with insecure HTTP", "node_id", *nodeID, "listen", *listen)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("agent stopped", "error", err)
			os.Exit(1)
		}
		return
	}
	log.Info("agent starting with mTLS", "node_id", *nodeID, "listen", *listen, "controller", *controllerURL)
	if err := httpServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		log.Error("agent stopped", "error", err)
		os.Exit(1)
	}
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
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
