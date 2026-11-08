package main

import (
	"flag"
	"log/slog"
	"os"

	"settle/internal/bank"
	"settle/internal/merchants"
	"settle/internal/payout"
)

func main() {
	dryRun := flag.Bool("preview", false, "plan the batch and send nothing")
	flag.Parse()
	store := merchants.NewStore()
	run := payout.NewRunner(store, bank.NewClient(os.Getenv("BANK_URL")))
	if err := run.Nightly(*dryRun); err != nil {
		slog.Error("nightly run failed", "error", err)
		os.Exit(1)
	}
}
