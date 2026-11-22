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
		// Written under a temporary name and renamed, so the test never reads a pid file that is still empty.
		pidFile, err := os.OpenFile("timeout-child.pid.tmp", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
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
		if err := os.Rename("timeout-child.pid.tmp", "timeout-child.pid"); err != nil {
			t.Fatalf("publish child ready file: %v", err)
		}
	}
	select {}
}
