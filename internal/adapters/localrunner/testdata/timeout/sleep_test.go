package timeoutpkg

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestSleep(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err == nil && cmd.Process != nil {
		pidFile := "__PID_FILE__"
		_ = os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
	}
	time.Sleep(30 * time.Second)
}
