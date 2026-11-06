// Command heimdall-runner serves bounded sandbox jobs to heimdall-server over a Unix socket.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/rootlessrunner"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "heimdall-runner: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	socket := flag.String("socket", "/run/heimdall-runner/runner.sock", "Unix socket path for the Heimdall server")
	goImage := flag.String("go-image", "", "approved Go image pinned by sha256 digest")
	profileDir := flag.String("profile-dir", "/etc/heimdall-runner/profiles", "operator-approved repository profiles")
	allowedImages := flag.String("allowed-images", "", "comma-separated immutable image references approved for profiles")
	flag.Parse()
	if *goImage == "" {
		return fmt.Errorf("--go-image is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	images := []string{*goImage}
	for _, image := range strings.Split(*allowedImages, ",") {
		if strings.TrimSpace(image) != "" {
			images = append(images, strings.TrimSpace(image))
		}
	}
	profiles, err := rootlessrunner.LoadApprovedProfiles(*profileDir, slices.Compact(images))
	if err != nil {
		return err
	}
	runner := rootlessrunner.Runner{Policy: rootlessrunner.Policy{Images: map[string]string{"go": *goImage}, Profiles: profiles}, Executor: rootlessrunner.Podman{}}
	if err := rootlessrunner.Listen(ctx, *socket, runner.Handler()); err != nil {
		return err
	}
	return nil
}
