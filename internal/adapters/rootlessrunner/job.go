package rootlessrunner

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The one job shape of checks and test kinds (REVIEW_ARCH 4.1, TEST_AGENT_ARCH 5.1): the head tree is extracted at
// /work, an optional base tree at /base, the profile's command runs in /work with no network, and its result file is
// read from /out. The command text comes only from a promoted profile and travels as an environment variable, never
// as part of the wrapper script, so it can neither break the script nor be confused with it.
const (
	// resultSARIF and resultJUnit are the result files of a check and of a test kind.
	resultSARIF = "/out/result.sarif"
	resultJUnit = "/out/junit.xml"

	// the markers frame the wrapper's own output after the command has ended.
	markLogBegin    = "heimdall-log-begin"
	markResultBegin = "heimdall-result-begin"
	markResultEnd   = "heimdall-result-end"
	markExit        = "heimdall-exit="
)

// profileJobScript is constant. The log tail is 256 KiB and the result file 2 MiB (ports.MaxJobResult); the full log
// never leaves the container. It extracts the trees, runs the command with its output sent to a file, then prints
// the log tail, the result file and the exit status between fixed markers. The command's own output cannot reach the
// markers' position: the wrapper prints them after the command has ended.
const profileJobScript = `set -u
extract() {
  strip=0
  top=$(tar -tzf "$1" | cut -d/ -f1 | sort -u)
  if [ "$(printf '%s\n' "$top" | wc -l)" = 1 ] && [ "$top" != . ] && tar -tzf "$1" | grep -qxF "$top/"; then strip=1; fi
  tar -xzf "$1" -C "$2" --strip-components="$strip" --no-same-owner --no-same-permissions
}
mkdir -p /work /base
extract /input/head.tgz /work || exit 97
if [ -f /input/base.tgz ]; then extract /input/base.tgz /base || exit 97; fi
if [ -d /input/overlays ]; then cp -a /input/overlays/. /work/; fi
cd /work
sh -c "$HEIMDALL_RUN" >/tmp/job.log 2>&1 </dev/null
code=$?
printf '\n` + markLogBegin + `\n'
tail -c 262144 /tmp/job.log
printf '\n` + markResultBegin + `\n'
head -c 2097152 "$HEIMDALL_RESULT" 2>/dev/null
printf '\n` + markResultEnd + `\n` + markExit + `%s\n' "$code"
`

// jobSpec is everything one profile-command job needs, already resolved from a promoted profile.
type jobSpec struct {
	Image       string
	Run         string
	ResultFile  string
	Archive     string // host path of the head archive
	BaseArchive string // host path of the base archive; empty when the job sees no base tree
	OverlayDir  string
	ModuleCache string
	Timeout     int
}

// profileJobArgs returns the Podman arguments of one profile-command job. Every input is checked again here, so a
// caller that bypasses the profile parser still cannot build a job outside the policy.
func (p Policy) profileJobArgs(j jobSpec) ([]string, error) {
	if !digestImage.MatchString(j.Image) {
		return nil, ErrImageNotAllowed
	}
	if err := checkJobCommand(j.Run); err != nil {
		return nil, err
	}
	if j.ResultFile != resultSARIF && j.ResultFile != resultJUnit {
		return nil, errors.New("job result file is not a known result file")
	}
	if err := j.checkPaths(); err != nil {
		return nil, err
	}
	if j.Timeout < 1 || j.Timeout > 600 {
		return nil, errors.New("sandbox timeout must be between 1 and 600 seconds")
	}
	args := hardenedArgs(j.Timeout)
	args = append(args,
		"--tmpfs", "/work:rw,nosuid,nodev,size=512m,mode=1777",
		"--tmpfs", "/base:rw,nosuid,nodev,size=256m,mode=1777",
		"--mount", "type=bind,src="+j.Archive+",dst=/input/head.tgz,ro=true",
		"--mount", "type=bind,src="+j.ModuleCache+",dst=/go/pkg/mod,ro=true",
	)
	if j.BaseArchive != "" {
		args = append(args, "--mount", "type=bind,src="+j.BaseArchive+",dst=/input/base.tgz,ro=true")
	}
	if j.OverlayDir != "" {
		args = append(args, "--mount", "type=bind,src="+j.OverlayDir+",dst=/input/overlays,ro=true")
	}
	args = append(args, "--env", "HEIMDALL_RUN="+j.Run, "--env", "HEIMDALL_RESULT="+j.ResultFile,
		j.Image, "/bin/sh", "-c", profileJobScript)
	return args, nil
}

// checkPaths requires every host path of the job to be absolute and clean with no character that could add a mount option.
func (j jobSpec) checkPaths() error {
	required := map[string]string{"source archive": j.Archive, "module cache": j.ModuleCache}
	optional := map[string]string{"base archive": j.BaseArchive, "overlay": j.OverlayDir}
	for name, path := range required {
		if err := absoluteCleanPath(path); err != nil {
			return fmt.Errorf("%s path: %w", name, err)
		}
	}
	for name, path := range optional {
		if path == "" {
			continue
		}
		if err := absoluteCleanPath(path); err != nil {
			return fmt.Errorf("%s path: %w", name, err)
		}
	}
	return nil
}

// checkJobCommand refuses a command the profile parser would already have refused.
func checkJobCommand(cmd string) error {
	if strings.TrimSpace(cmd) == "" || len(cmd) > 1024 || strings.ContainsAny(cmd, "\x00\r\n") {
		return errors.New("job command must be one bounded line")
	}
	return nil
}

// safeJobDir is the directory a {dir} placeholder may stand for: plain path characters only, so it can sit inside a
// shell command without quoting.
var safeJobDir = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// resolveDir validates the repository-relative directory a kind's {dir} names.
func resolveDir(dir, fallback string) (string, error) {
	if dir == "" {
		dir = fallback
	}
	rel, err := cleanRelDir(dir)
	if err != nil {
		return "", err
	}
	if !safeJobDir.MatchString(rel) {
		return "", errors.New("job directory escape is refused")
	}
	return rel, nil
}

// jobOutput is what a profile-command job printed after its command ended.
type jobOutput struct {
	Log    []byte
	Result []byte // the result file; empty when the command wrote none
	Exit   int
}

// parseJobOutput splits the wrapper output at its markers, reading from the end: the genuine trailer is the last
// thing printed. Output without a trailer is not a result: the job was killed or never reached the command.
func parseJobOutput(out []byte) (jobOutput, bool) {
	exitAt := bytes.LastIndex(out, []byte("\n"+markExit))
	if exitAt < 0 {
		return jobOutput{}, false
	}
	exit, err := strconv.Atoi(strings.TrimSpace(string(out[exitAt+1+len(markExit):])))
	if err != nil {
		return jobOutput{}, false
	}
	head := out[:exitAt]
	endAt := bytes.LastIndex(head, []byte("\n"+markResultEnd))
	if endAt < 0 {
		return jobOutput{}, false
	}
	body := head[:endAt]
	beginAt := bytes.LastIndex(body, []byte("\n"+markResultBegin+"\n"))
	if beginAt < 0 {
		return jobOutput{}, false
	}
	res := jobOutput{Result: body[beginAt+len(markResultBegin)+2:], Exit: exit}
	logPart := body[:beginAt]
	if i := bytes.LastIndex(logPart, []byte(markLogBegin+"\n")); i >= 0 {
		logPart = logPart[i+len(markLogBegin)+1:]
	}
	res.Log = logPart
	return res, true
}
