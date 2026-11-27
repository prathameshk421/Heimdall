package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/digest"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// digestNow is the instant the digest tests read as the clock, so the default period is fixed.
var digestNow = time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)

// digestDepsFor is the digest step wired to a fixed clock and no GitHub.
func digestDepsFor(env map[string]string) digestDeps {
	return digestDeps{getenv: func(k string) string { return env[k] }, http: &http.Client{Timeout: time.Second},
		wait: noWait, now: func() time.Time { return digestNow },
		state: func(context.Context, digestSource, io.Writer) (digest.Input, error) {
			return digest.Input{}, errNoState
		}}
}

// errNoState is what a test's state reader returns when a test did not expect a GitHub read.
var errNoState = jsonError("no state reader in this test")

// noPolicyMode reads no policy at all, so the mode of a change without a base is the default: shadow.
func noPolicyMode(context.Context, id.SHA) string { return "" }

// jsonError is an error a test can compare by text without a new package-level value.
type jsonError string

func (e jsonError) Error() string { return string(e) }

// runDigest runs `heimdall action digest` with the given arguments and returns its status and both streams.
func runDigest(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return runDigestWith(t, digestDepsFor(nil), args...)
}

func runDigestWith(t *testing.T, deps digestDeps, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runActionDigest(context.Background(), args, &out, &errOut, deps)
	return code, out.String(), errOut.String()
}

// snapshot is a week of three pull requests: one clean, one that would block on a real finding, one whose proof run
// ended at the budget. It is the file the -in tests read.
func snapshot() digest.Input {
	from := digestNow.AddDate(0, 0, -7)
	return digest.Input{Repo: "acme/pay", From: from, To: digestNow, PRs: []digest.PRRecord{
		{Number: 11, Head: "a1b2c3d4", Mode: digest.ModeShadow, At: digestNow.AddDate(0, 0, -2),
			Fast: digest.FastRecord{Ran: true, Clauses: []digest.Clause{
				{ID: fastcheck.ClauseSecrets, Status: fastcheck.StatusMet},
				{ID: fastcheck.ClauseDiffCoverage, Status: fastcheck.StatusMet},
			}}, FastTimeMS: 40_000, CostMicros: 200_000},
		{Number: 12, Head: "b2c3d4e5", Mode: digest.ModeShadow, At: digestNow.AddDate(0, 0, -1),
			Fast: digest.FastRecord{Ran: true, Clauses: []digest.Clause{
				{ID: fastcheck.ClauseSecrets, Status: fastcheck.StatusUnmet, Reason: "a token is committed"},
			}}, FastTimeMS: 45_000,
			Proof: digest.ProofRecord{Ran: true, Kind: proof.KindBlocked,
				Counts: digest.Counts{In: 2, Proven: 1, Blocking: 1, NoProof: 1}},
			ProofTimeMS: 300_000, CostMicros: 400_000},
		{Number: 13, Head: "c3d4e5f6", Mode: digest.ModeShadow, At: digestNow.AddDate(0, 0, -3),
			Fast: digest.FastRecord{Ran: true, Clauses: []digest.Clause{
				{ID: fastcheck.ClauseApproval, Status: fastcheck.StatusMet},
			}}, FastTimeMS: 50_000,
			Proof: digest.ProofRecord{Ran: true, Kind: proof.KindUnknown, Reason: proof.ReasonBudget,
				Counts:            digest.Counts{In: 3, NotChecked: 3},
				NotCheckedReasons: []digest.ReasonTally{{Reason: proof.ReasonBudget, Count: 3}}},
			ProofTimeMS: 420_000, CostMicros: 500_000},
	}}
}

// writeSnapshot writes the snapshot to a temporary file and returns its path.
func writeSnapshot(t *testing.T, in digest.Input) string {
	t.Helper()
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	p := filepath.Join(t.TempDir(), "digest.json")
	require.NoError(t, os.WriteFile(p, raw, 0o600))
	return p
}

// TestDigestReadsASnapshotAsJSON: the JSON form is the same numbers as the page, and it carries the fields a consumer
// reads: the counts, the causes and the misses.
func TestDigestReadsASnapshotAsJSON(t *testing.T) {
	path := writeSnapshot(t, snapshot())
	code, stdout, stderr := runDigest(t, "-in", path, "-format", "json")
	require.Equal(t, exitActionOK, code, stderr)
	var got digest.Report
	require.NoError(t, json.Unmarshal([]byte(stdout), &got), "the output must be valid JSON")
	require.Equal(t, "acme/pay", got.Repo)
	require.Equal(t, 3, got.PRs)
	require.Equal(t, 3, got.Shadow.Checked)
	require.Equal(t, 1, got.Shadow.WouldPass)
	require.Equal(t, 2, got.Shadow.WouldBlock)
	require.Equal(t, 1, got.Shadow.RunBlocked)
	require.Equal(t, 1, got.Shadow.RunUnknown)
	require.Equal(t, int64(1_100_000), got.Shadow.Cost.TotalMicros)
	require.Equal(t, 3, got.Shadow.Cost.Measured)
	require.Equal(t, []digest.Cause{
		{Kind: digest.CauseFastUnmet, ID: string(fastcheck.ClauseSecrets), Count: 1},
		{Kind: digest.CauseProofBlocking, Count: 1},
		{Kind: digest.CauseProofUnknown, ID: string(proof.ReasonBudget), Count: 1},
	}, got.Shadow.Causes)
	require.Equal(t, 3, got.Misses.CandidateNotChecked[0].Count)
	require.Equal(t, string(proof.ReasonBudget), got.Misses.CandidateNotChecked[0].Reason)
	require.Equal(t, int64(50_000), got.Misses.Fast.MaxMS)
}

// TestDigestReadsASnapshotAsMarkdown: the default format is the page, and it names both halves of the report.
func TestDigestReadsASnapshotAsMarkdown(t *testing.T) {
	path := writeSnapshot(t, snapshot())
	code, stdout, stderr := runDigest(t, "-in", path)
	require.Equal(t, exitActionOK, code, stderr)
	require.Contains(t, stdout, "# Shadow digest: `acme/pay`")
	require.Contains(t, stdout, "## Shadow digest")
	require.Contains(t, stdout, "## What we did not check")
	require.Contains(t, stdout, "would pass: 1 of 3")
	require.Contains(t, stdout, "would block: 2 of 3")
	require.Contains(t, stdout, "fast check unmet: `secrets`")
	require.Contains(t, stdout, "budget hit: 3 candidates")
	require.Contains(t, stdout, "Proof run wall time: p50")
	require.LessOrEqual(t, len(stdout), digest.MaxBytes)
}

// TestDigestSnapshotKeepsItsOwnWindow: a snapshot of a past week is what a weekly report is about, so its window wins.
func TestDigestSnapshotKeepsItsOwnWindow(t *testing.T) {
	in := snapshot()
	in.From = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	path := writeSnapshot(t, in)
	code, stdout, stderr := runDigest(t, "-in", path, "-days", "1", "-format", "json")
	require.Equal(t, exitActionOK, code, stderr)
	var got digest.Report
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	require.Equal(t, in.From.UTC(), got.From.UTC())
	require.Equal(t, 3, got.PRs)
}

// TestDigestEmptySnapshot: an empty period renders a page that claims nothing, and never a readiness verdict.
func TestDigestEmptySnapshot(t *testing.T) {
	path := writeSnapshot(t, digest.Input{Repo: "acme/pay"})
	code, stdout, stderr := runDigest(t, "-in", path)
	require.Equal(t, exitActionOK, code, stderr)
	require.Contains(t, stdout, "No pull request was checked in this period")
	require.NotContains(t, stdout, "readiness: the check is", "a period nobody checked claims no readiness")
}

// TestDigestUsageErrors: a bad flag, a bad format, two sources and no source are all refused with a usage status, so a
// mistyped run never reports a number somebody would read as real.
func TestDigestUsageErrors(t *testing.T) {
	path := writeSnapshot(t, snapshot())
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "bad format", args: []string{"-in", path, "-format", "yaml"}, want: "neither md nor json"},
		{name: "negative days", args: []string{"-in", path, "-days", "-1"}, want: "must be 0 or more"},
		{name: "two sources", args: []string{"-in", path, "-repo", "acme/pay"}, want: "name two sources"},
		{name: "a stray argument", args: []string{"-in", path, "extra"}, want: "unexpected argument"},
		{name: "an unknown flag", args: []string{"-nope"}, want: "flag provided but not defined"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _, stderr := runDigest(t, c.args...)
			require.Equal(t, exitActionUsage, code)
			require.Contains(t, stderr, c.want)
		})
	}
}

// TestDigestReportsNoSource: with neither flag the step says what to give, and it does not invent a period of zero
// records that would read as a clean week.
func TestDigestReportsNoSource(t *testing.T) {
	code, stdout, stderr := runDigest(t)
	require.Equal(t, exitActionFail, code)
	require.Empty(t, stdout)
	require.Contains(t, stderr, "no source")
	require.Contains(t, stderr, "-in")
	require.Contains(t, stderr, "-repo")
}

// TestDigestReportsAMissingSnapshot: a path that does not exist is an error, not an empty report.
func TestDigestReportsAMissingSnapshot(t *testing.T) {
	code, _, stderr := runDigest(t, "-in", filepath.Join(t.TempDir(), "absent.json"))
	require.Equal(t, exitActionFail, code)
	require.Contains(t, stderr, "open")
}

// TestDigestReportsABrokenSnapshot: a file that is not the shape is an error that names the file and the time format.
func TestDigestReportsABrokenSnapshot(t *testing.T) {
	p := filepath.Join(t.TempDir(), "broken.json")
	require.NoError(t, os.WriteFile(p, []byte(`{"records": [{"number": "eleven"}]}`), 0o600))
	code, _, stderr := runDigest(t, "-in", p)
	require.Equal(t, exitActionFail, code)
	require.Contains(t, stderr, "broken.json")
	require.Contains(t, stderr, "RFC 3339")
}

// TestDigestRefusesAReportAsASnapshot: a rendered report is not a snapshot, and reading one back would give an empty
// period that reads as a clean week. A snapshot with no records at all is refused for the same reason.
func TestDigestRefusesAReportAsASnapshot(t *testing.T) {
	path := writeSnapshot(t, snapshot())
	_, report, stderr := runDigest(t, "-in", path, "-format", "json")
	require.Empty(t, stderr)
	p := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, os.WriteFile(p, []byte(report), 0o600))
	code, _, stderr := runDigest(t, "-in", p)
	require.Equal(t, exitActionFail, code)
	require.Contains(t, stderr, "a report, not a snapshot")

	empty := filepath.Join(t.TempDir(), "empty.json")
	require.NoError(t, os.WriteFile(empty, []byte(`{"repo": "acme/pay"}`), 0o600))
	code, _, stderr = runDigest(t, "-in", empty)
	require.Equal(t, exitActionFail, code)
	require.Contains(t, stderr, "no records")
}

// TestDigestReportsABadRepo: a repository name that is not owner/name is refused before any GitHub call.
func TestDigestReportsABadRepo(t *testing.T) {
	code, _, stderr := runDigest(t, "-repo", "not-a-repo")
	require.Equal(t, exitActionFail, code)
	require.Contains(t, stderr, "owner/name")
}

// TestDigestStateRefIsBehindItsOwnReader: the -repo form goes through the deps' state reader, so the flag, the window
// and the report are tested without a GitHub call.
func TestDigestStateRefUsesTheConfiguredReader(t *testing.T) {
	deps := digestDepsFor(nil)
	var got digestSource
	deps.state = func(_ context.Context, src digestSource, _ io.Writer) (digest.Input, error) {
		got = src
		return digest.Input{Repo: src.repo.String(), From: src.window.From, To: src.window.To, PRs: []digest.PRRecord{
			{Number: 1, At: digestNow.Add(-time.Hour), Fast: digest.FastRecord{Ran: true}},
		}}, nil
	}
	code, stdout, stderr := runDigestWith(t, deps, "-repo", "acme/pay", "-days", "3")
	require.Equal(t, exitActionOK, code, stderr)
	require.Equal(t, "acme/pay", got.repo.String())
	require.Equal(t, "refs/heimdall/state", got.ref)
	require.Equal(t, digestNow.AddDate(0, 0, -3), got.window.From)
	require.Contains(t, stdout, "1 pull request in this period. Checked: 1.")
}

// TestDigestStateRefReportsAReaderFailure: a read that fails fails the step, so a failed read is never a clean week.
func TestDigestStateRefReportsAReaderFailure(t *testing.T) {
	deps := digestDepsFor(nil)
	code, stdout, stderr := runDigestWith(t, deps, "-repo", "acme/pay")
	require.Equal(t, exitActionFail, code)
	require.Empty(t, stdout)
	require.Contains(t, stderr, string(errNoState))
}

// TestRecordOfReadsAChangeView: the extraction from a change.Viex is what the -repo form does per change, so it is
// checked on the view the digest is really about: a fast check, a finished proof run and its wall time.
func TestRecordOfReadsAChangeView(t *testing.T) {
	events := digestChangeEvents(t)
	rec, ok := recordOf(context.Background(), events, noPolicyMode)
	require.True(t, ok)
	require.Equal(t, 7, rec.Number)
	require.True(t, rec.Fast.Ran)
	require.Len(t, rec.Fast.Clauses, 1)
	require.True(t, rec.Proof.Ran)
	require.Equal(t, proof.KindBlocked, rec.Proof.Kind)
	require.Equal(t, 1, rec.Proof.Counts.Blocking)
	require.Equal(t, int64(250_000), rec.CostMicros)
	require.Equal(t, int64(300_000), rec.ProofTimeMS, "the wall time is the start to the report of one run")
	require.Equal(t, digest.ModeShadow, rec.Mode, "no base policy is read here, so the default mode stands")
	require.Equal(t, 1, rec.BreakGlassOpen)
}

// TestRecordOfSkipsAChangeWithNoPullRequest: a ticket stream with no pull request is not counted as a pull request.
func TestRecordOfSkipsAChangeWithNoPullRequest(t *testing.T) {
	events := digestChangeEvents(t)[1:2] // a fast check with no pull request in the log
	_, ok := recordOf(context.Background(), events, noPolicyMode)
	require.False(t, ok)
}

// TestProofWallTimeNeedsBothEnds: a proof run with no start event, or one whose report is not later, has no
// measurement, so the report says "not measured" and never a zero that reads as instant.
func TestProofWallTimeNeedsBothEnds(t *testing.T) {
	require.Equal(t, int64(0), proofWallMS(nil, nil))
	view := changeViewOf(t, digestChangeEvents(t))
	require.Equal(t, int64(300_000), proofWallMS(digestChangeEvents(t), view.Action))
	reportedOnly := digestChangeEvents(t)
	require.Equal(t, int64(0), proofWallMS([]change.Event{reportedOnly[len(reportedOnly)-2]}, view.Action),
		"a report with no start has no measurement")
}

// TestByNewestPR and TestChangeIDsIn keep the order of a tree listing from mattering.

func TestByNewestPR(t *testing.T) {
	require.True(t, byNewestPR("pr-acme-pay-12", "pr-acme-pay-9"), "12 is newer than 9, whatever the text says")
	require.False(t, byNewestPR("pr-acme-pay-9", "pr-acme-pay-12"))
	require.True(t, byNewestPR("pr-acme-pay-1", "PAY-142"), "a ticket stream is read after the pull requests")
	require.False(t, byNewestPR("PAY-142", "pr-acme-pay-1"))
	require.True(t, byNewestPR("PAY-143", "PAY-142"))
}

func TestChangeIDsIn(t *testing.T) {
	items := []ports.GitTreeItem{
		{Path: "changes/pr-acme-pay-9.jsonl", Type: "blob"},
		{Path: "changes/pr-acme-pay-12.jsonl", Type: "blob"},
		{Path: "changes/pr-acme-pay-12.head", Type: "blob"},
		{Path: "effects/pr-acme-pay-12.jsonl", Type: "blob"},
		{Path: "changes", Type: "tree"},
		{Path: "changes/not-a-change.jsonl", Type: "blob"},
		{Path: "budget/2026-11.json", Type: "blob"},
	}
	ids := changeIDsIn(items)
	require.Equal(t, []id.ChangeID{"pr-acme-pay-12", "pr-acme-pay-9"}, ids)
}

// TestLogKeyOf: the key comes from the environment, is hex, and is long enough for the store. An error never prints it.
func TestLogKeyOf(t *testing.T) {
	good := strings.Repeat("ab", 32)
	key, err := logKeyOf(func(k string) string { return map[string]string{"HEIMDALL_LOG_KEY": good}[k] })
	require.NoError(t, err)
	require.Len(t, key, 32)
	_, err = logKeyOf(func(string) string { return "" })
	require.ErrorContains(t, err, "-in")
	_, err = logKeyOf(func(string) string { return "zz" })
	require.ErrorContains(t, err, "must be hex")
	_, err = logKeyOf(func(string) string { return "abcd" })
	require.ErrorContains(t, err, "at least")
}
