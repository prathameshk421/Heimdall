package rootlessrunner_test

import (
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/rootlessrunner"
	"github.com/stretchr/testify/require"
)

func TestContainerArgsEnforceIsolationAndRejectUnapprovedImages(t *testing.T) {
	policy := rootlessrunner.Policy{Images: map[string]string{"go": "registry.example/go@sha256:" + strings.Repeat("a", 64)}}
	args, err := policy.ContainerArgs("go", "/tmp/source", 30)
	require.NoError(t, err)
	require.Contains(t, args, "--network=none")
	require.Contains(t, args, "type=bind,src=/var/lib/heimdall-runner/module-cache,dst=/go/pkg/mod,ro=true")
	require.Contains(t, args, "--read-only")
	require.Contains(t, args, "--cap-drop=ALL")
	require.Contains(t, args, "--security-opt=no-new-privileges")
	require.Contains(t, args, "--pids-limit=512")
	require.Contains(t, args, "--memory=2g")
	require.Contains(t, args, "--cpus=2")
	require.Contains(t, args, "/out:rw,noexec,nosuid,nodev,size=8m,mode=1777")
	require.NotContains(t, args, "/var/run/docker.sock")
	require.NotContains(t, args, "169.254.169.254")
	require.NotContains(t, args, "--privileged")
	require.NotContains(t, args, "--device")

	_, err = policy.ContainerArgs("evil", "/tmp/source", 30)
	require.ErrorIs(t, err, rootlessrunner.ErrImageNotAllowed)
}

// A GitHub tarball unpacks into one owner-repo-sha directory. The job must enter that directory
// before it runs the acceptance command, and overlays are repository-relative so they land inside it.
func TestContainerArgsEnterTheExtractedArchiveRoot(t *testing.T) {
	policy := rootlessrunner.Policy{Images: map[string]string{"go": "registry.example/go@sha256:" + strings.Repeat("a", 64)}}
	args, err := policy.ContainerArgs("go", "/tmp/source", 30)
	require.NoError(t, err)

	script := args[len(args)-1]
	require.Contains(t, script, "root='/work/src'", "the job resolves the extracted archive root")
	require.Contains(t, script, `cd "$root" && go test -json`, "the acceptance command runs at the archive root")
}

func TestContainerArgsCopyOverlaysIntoTheArchiveRoot(t *testing.T) {
	policy := rootlessrunner.Policy{Images: map[string]string{"go": "registry.example/go@sha256:" + strings.Repeat("a", 64)}}
	args, err := policy.ContainerArgsWithOverlays("go", "/tmp/source", "/tmp/overlays", "/tmp/module-cache", "tests/heimdall/KAN-1", 30)
	require.NoError(t, err)

	script := args[len(args)-1]
	require.Contains(t, script, `cp -a /input/overlays/. "$root/"`, "overlays are repository-relative, so they copy into the archive root")
}

// A cold Go build writes hundreds of megabytes of build cache and temporary files. /tmp is only
// 64 MiB, so both must live on the larger work tmpfs.
func TestContainerArgsKeepTheGoBuildCacheOffTheSmallTmp(t *testing.T) {
	policy := rootlessrunner.Policy{Images: map[string]string{"go": "registry.example/go@sha256:" + strings.Repeat("a", 64)}}
	args, err := policy.ContainerArgs("go", "/tmp/source", 30)
	require.NoError(t, err)

	script := args[len(args)-1]
	require.Contains(t, script, "GOCACHE=/work/.gocache")
	require.Contains(t, script, "GOTMPDIR=/work/.gotmp")
	require.NotContains(t, script, "GOCACHE=/tmp/")
}

func TestContainerArgsRejectMountEscapeAndUnsafeTimeout(t *testing.T) {
	policy := rootlessrunner.Policy{Images: map[string]string{"go": "registry.example/go@sha256:" + strings.Repeat("a", 64)}}
	_, err := policy.ContainerArgs("go", "/tmp/source,evil", 30)
	require.Error(t, err)
	_, err = policy.ContainerArgs("go", "/tmp/source", 601)
	require.Error(t, err)
}
