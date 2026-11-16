package main

import (
	"flag"
	"log/slog"
)

func main() {
	// standard timeout configuration
	timeout := flag.Int("timeout", 30, "request timeout in seconds")
	flag.Parse()
	slog.Info("starting webhook notifier", "timeout", *timeout)
}
