package localrunner_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/localrunner"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestParseEventsPassingTest(t *testing.T) {
	stream := strings.Join([]string{
		`{"Time":"2026-11-05T00:00:00Z","Action":"start","Package":"acme/pkg"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"run","Package":"acme/pkg","Test":"TestAdd"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"output","Package":"acme/pkg","Test":"TestAdd","Output":"=== RUN   TestAdd\n"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"output","Package":"acme/pkg","Test":"TestAdd","Output":"--- PASS: TestAdd (0.01s)\n"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"pass","Package":"acme/pkg","Test":"TestAdd","Elapsed":0.01}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"pass","Package":"acme/pkg","Elapsed":0.01}`,
	}, "\n")

	rep, log, err := localrunner.ParseEvents(strings.NewReader(stream))
	require.NoError(t, err)
	require.NotEmpty(t, log)

	cases := rep.Cases()
	require.Len(t, cases, 1)
	require.Equal(t, "TestAdd", cases[0].Name)
	require.Equal(t, verify.StatusPass, cases[0].Status)
	require.Empty(t, cases[0].Message)

	res := localrunner.ReportToSandboxResult(rep, workflow.BlobRef("blob:sha256:"+strings.Repeat("a", 64)))
	require.NoError(t, res.Validate())
	require.Equal(t, 1, res.Passed)
	require.Equal(t, 0, res.Failed)
	require.Equal(t, 0, res.Errored)
	require.Len(t, res.Tests, 1)
	require.Equal(t, ports.OutcomePassed, res.Tests[0].Outcome)
}

func TestParseEventsAssertionFailure(t *testing.T) {
	stream := strings.Join([]string{
		`{"Time":"2026-11-05T00:00:00Z","Action":"start","Package":"acme/pkg"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"run","Package":"acme/pkg","Test":"TestMinus"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"output","Package":"acme/pkg","Test":"TestMinus","Output":"=== RUN   TestMinus\n"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"output","Package":"acme/pkg","Test":"TestMinus","Output":"    minus_test.go:12: want 5 got 3\n"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"output","Package":"acme/pkg","Test":"TestMinus","Output":"--- FAIL: TestMinus (0.01s)\n"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"fail","Package":"acme/pkg","Test":"TestMinus","Elapsed":0.01}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"fail","Package":"acme/pkg","Elapsed":0.01}`,
	}, "\n")

	rep, _, err := localrunner.ParseEvents(strings.NewReader(stream))
	require.NoError(t, err)

	cases := rep.Cases()
	require.Len(t, cases, 1)
	require.Equal(t, "TestMinus", cases[0].Name)
	require.Equal(t, verify.StatusFail, cases[0].Status)
	require.Contains(t, cases[0].Message, "want 5 got 3")

	res := localrunner.ReportToSandboxResult(rep, workflow.BlobRef("blob:sha256:"+strings.Repeat("b", 64)))
	require.NoError(t, res.Validate())
	require.Equal(t, 0, res.Passed)
	require.Equal(t, 1, res.Failed)
	require.Equal(t, 0, res.Errored)
	require.Equal(t, ports.OutcomeFailure, res.Tests[0].Outcome)
}

func TestParseEventsPackageBuildFailure(t *testing.T) {
	stream := strings.Join([]string{
		`{"ImportPath":"acme/pkg.test","Action":"build-output","Output":"# acme/pkg\n"}`,
		`{"ImportPath":"acme/pkg.test","Action":"build-output","Output":"pkg_test.go:4:2: undefined: Missing\n"}`,
		`{"ImportPath":"acme/pkg.test","Action":"build-fail"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"start","Package":"acme/pkg"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"output","Package":"acme/pkg","Output":"FAIL\tacme/pkg [setup failed]\n"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"fail","Package":"acme/pkg","Elapsed":0,"FailedBuild":"acme/pkg.test"}`,
	}, "\n")

	rep, _, err := localrunner.ParseEvents(strings.NewReader(stream))
	require.NoError(t, err)

	cases := rep.Cases()
	require.Len(t, cases, 1)
	require.Equal(t, "[build failed]", cases[0].Name)
	require.Equal(t, verify.StatusError, cases[0].Status)
	require.Contains(t, cases[0].Message, "undefined: Missing")

	res := localrunner.ReportToSandboxResult(rep, workflow.BlobRef("blob:sha256:"+strings.Repeat("c", 64)))
	require.NoError(t, res.Validate())
	require.Equal(t, 0, res.Passed)
	require.Equal(t, 0, res.Failed)
	require.Equal(t, 1, res.Errored)
	require.Equal(t, ports.OutcomeError, res.Tests[0].Outcome)
}

func TestParseEventsPanicIsError(t *testing.T) {
	stream := strings.Join([]string{
		`{"Time":"2026-11-05T00:00:00Z","Action":"start","Package":"acme/pkg"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"run","Package":"acme/pkg","Test":"TestCrash"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"output","Package":"acme/pkg","Test":"TestCrash","Output":"=== RUN   TestCrash\n"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"output","Package":"acme/pkg","Test":"TestCrash","Output":"panic: nil pointer dereference\n"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"fail","Package":"acme/pkg","Test":"TestCrash","Elapsed":0.01}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"fail","Package":"acme/pkg","Elapsed":0.01}`,
	}, "\n")

	rep, _, err := localrunner.ParseEvents(strings.NewReader(stream))
	require.NoError(t, err)

	cases := rep.Cases()
	require.Len(t, cases, 1)
	require.Equal(t, "TestCrash", cases[0].Name)
	require.Equal(t, verify.StatusError, cases[0].Status)
	require.Contains(t, cases[0].Message, "panic:")

	res := localrunner.ReportToSandboxResult(rep, workflow.BlobRef("blob:sha256:"+strings.Repeat("d", 64)))
	require.Equal(t, 0, res.Passed)
	require.Equal(t, 0, res.Failed)
	require.Equal(t, 1, res.Errored)
	require.Equal(t, ports.OutcomeError, res.Tests[0].Outcome)
}

func TestParseEventsSkippedTest(t *testing.T) {
	stream := strings.Join([]string{
		`{"Time":"2026-11-05T00:00:00Z","Action":"start","Package":"acme/pkg"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"run","Package":"acme/pkg","Test":"TestSkipMe"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"output","Package":"acme/pkg","Test":"TestSkipMe","Output":"=== RUN   TestSkipMe\n"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"output","Package":"acme/pkg","Test":"TestSkipMe","Output":"    skip_test.go:5: not supported on this OS\n"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"skip","Package":"acme/pkg","Test":"TestSkipMe","Elapsed":0.01}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"pass","Package":"acme/pkg","Elapsed":0.01}`,
	}, "\n")

	rep, _, err := localrunner.ParseEvents(strings.NewReader(stream))
	require.NoError(t, err)

	cases := rep.Cases()
	require.Len(t, cases, 1)
	require.Equal(t, "TestSkipMe", cases[0].Name)
	require.Equal(t, verify.StatusSkipped, cases[0].Status)

	res := localrunner.ReportToSandboxResult(rep, workflow.BlobRef("blob:sha256:"+strings.Repeat("e", 64)))
	require.Equal(t, 0, res.Passed)
	require.Equal(t, 0, res.Failed)
	require.Equal(t, 0, res.Errored)
	require.Len(t, res.Tests, 1)
	require.Equal(t, ports.OutcomeSkipped, res.Tests[0].Outcome)
}

func TestParseEventsUnfinishedTestIsError(t *testing.T) {
	// A test that started (run) but the process terminated before pass/fail/skip
	stream := strings.Join([]string{
		`{"Time":"2026-11-05T00:00:00Z","Action":"start","Package":"acme/pkg"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"run","Package":"acme/pkg","Test":"TestHung"}`,
		`{"Time":"2026-11-05T00:00:00Z","Action":"output","Package":"acme/pkg","Test":"TestHung","Output":"=== RUN   TestHung\n"}`,
	}, "\n")

	rep, _, err := localrunner.ParseEvents(strings.NewReader(stream))
	require.NoError(t, err)

	cases := rep.Cases()
	require.Len(t, cases, 1)
	require.Equal(t, "TestHung", cases[0].Name)
	require.Equal(t, verify.StatusError, cases[0].Status)

	res := localrunner.ReportToSandboxResult(rep, workflow.BlobRef("blob:sha256:"+strings.Repeat("f", 64)))
	require.Equal(t, 0, res.Passed)
	require.Equal(t, 0, res.Failed)
	require.Equal(t, 1, res.Errored)
	require.Equal(t, ports.OutcomeError, res.Tests[0].Outcome)
}
