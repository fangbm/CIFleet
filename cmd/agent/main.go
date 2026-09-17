package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"

	"github.com/fangbm/cifleet/internal/agent"
)

func main() {
	listen := flag.String("listen", ":8090", "agent listen address")
	nodeID := flag.String("node-id", "node", "unique node id")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := &agent.Server{NodeID: *nodeID}
	log.Info("agent starting", "node_id", *nodeID, "listen", *listen)
	if err := http.ListenAndServe(*listen, server.Handler()); err != nil {
		log.Error("agent stopped", "error", err)
		os.Exit(1)
	}
}
