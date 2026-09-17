package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"

	"github.com/fangbm/cifleet/internal/controller"
)

func main() {
	listen := flag.String("listen", ":8080", "controller listen address")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := controller.New(log)
	log.Info("controller starting", "listen", *listen)
	if err := http.ListenAndServe(*listen, server.Handler()); err != nil {
		log.Error("controller stopped", "error", err)
		os.Exit(1)
	}
}
