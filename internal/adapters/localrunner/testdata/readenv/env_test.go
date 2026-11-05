package readenv

import (
	"os"
	"testing"
)

func TestEnvNotForwarded(t *testing.T) {
	for _, key := range []string{"SECRET_TOKEN", "PARENT_SECRET", "HEIMDALL_SECRET", "USER", "SHELL"} {
		if val := os.Getenv(key); val != "" {
			t.Fatalf("parent environment variable %s leaked: %q", key, val)
		}
	}
}
