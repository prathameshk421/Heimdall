package timeoutpkg

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
)

func TestSleep(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err == nil && cmd.Process != nil {
		pidFile, err := os.OpenFile("timeout-child.pid", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			t.Fatalf("create child ready file: %v", err)
		}
		if _, err := pidFile.WriteString(strconv.Itoa(cmd.Process.Pid)); err != nil {
			t.Fatalf("write child pid: %v", err)
		}
		if err := pidFile.Sync(); err != nil {
			t.Fatalf("sync child ready file: %v", err)
		}
		if err := pidFile.Close(); err != nil {
			t.Fatalf("close child ready file: %v", err)
		}
	}
	select {}
}
