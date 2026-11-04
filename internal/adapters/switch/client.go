package switchchat

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Secrets reads one secret by file name. config.Config satisfies it.
type Secrets interface {
	Secret(name string) (string, error)
}

// Config is what New needs. BaseURL is the bridge base URL (the agent bridge, not the Gateway). Secrets is read on
// every call, so a rotated key file is picked up without a restart.
type Config struct {
	BaseURL            string
	AgentID            string
	AgentName          string
	BridgeID           string
	Secrets            Secrets
	APIKeySecret       string
	RegistrationSecret string
	HTTPClient         *http.Client
	Clock              ports.Clock
	Wait               func(ctx context.Context, d time.Duration) error
}

// requestDeadline bounds every call except the event stream itself (TECH_SPEC 2.5).
const requestDeadline = 10 * time.Second

// Client talks to one Switch bridge as the coordinator agent. It is safe for concurrent use.
type Client struct {
	secrets  Secrets
	bridgeID string
}

// StatusOf returns the HTTP status of a Switch reply carried by err, if there is one.
func StatusOf(err error) (int, bool) {
	return 0, false
}

// New builds the client. It fails on a missing dependency or a base URL that is not absolute. Nothing is sent.
func New(cfg Config) (*Client, error) {
	return nil, errors.New("not implemented")
}

// Stream-level error sentinels are declared in stream.go.

var _ = change.RoomID("")
