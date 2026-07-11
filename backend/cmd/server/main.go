// Command server runs the E2 HTTP API (interface-contract.md §5).
// TODO: wire Postgres (backend/db/schema.sql), the crank Submitter once the Anchor
// program is deployed to devnet, and the feed provider (replay by default, txodds
// once docs/txodds-day1-email.md access lands).
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/Zerith-Studio/prediction-market/backend/internal/api"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	addr := os.Getenv("PITCHMARKET_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	srv := api.New(log)
	log.Info("starting server", "addr", addr)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Error("server exited", "err", err)
		os.Exit(1)
	}
}
