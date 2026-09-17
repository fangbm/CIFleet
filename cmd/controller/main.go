package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/fangbm/cifleet/internal/controller"
	"github.com/fangbm/cifleet/internal/tlsutil"
)

func main() {
	listen := flag.String("listen", env("CIFLEET_LISTEN", ":8080"), "controller listen address")
	caFile := flag.String("ca-file", env("CIFLEET_CA_FILE", "/etc/cifleet/pki/ca.crt"), "trusted client CA")
	certFile := flag.String("cert-file", env("CIFLEET_CERT_FILE", "/etc/cifleet/pki/controller.crt"), "server certificate")
	keyFile := flag.String("key-file", env("CIFLEET_KEY_FILE", "/etc/cifleet/pki/controller.key"), "server private key")
	nodeTTL := flag.Duration("node-ttl", envDuration("CIFLEET_NODE_TTL", 45*time.Second), "mark a node offline after this interval")
	insecureHTTP := flag.Bool("insecure-http", false, "serve plaintext HTTP without client certificate authentication (development only)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := controller.New(log, *nodeTTL)
	httpServer := &http.Server{Addr: *listen, Handler: server.Handler(), ReadHeaderTimeout: 5 * time.Second}

	if *insecureHTTP {
		log.Warn("controller starting with insecure HTTP", "listen", *listen)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("controller stopped", "error", err)
			os.Exit(1)
		}
		return
	}

	tlsConfig, err := tlsutil.LoadServerConfig(*caFile, *certFile, *keyFile)
	if err != nil {
		log.Error("load mTLS configuration", "error", err)
		os.Exit(1)
	}
	httpServer.TLSConfig = tlsConfig
	log.Info("controller starting with mTLS", "listen", *listen, "node_ttl", nodeTTL.String())
	if err := httpServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		log.Error("controller stopped", "error", err)
		os.Exit(1)
	}
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
