package main

import (
	"flag"
	"log/slog"
	"os"
)

func main() {
	timeout := flag.Int("max-timeout", 30, "request timeout in seconds")
	flag.Parse()
	slog.Info("starting webhook notifier", "timeout", *timeout)
}
