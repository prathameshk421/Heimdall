package localrunner

import (
	"io"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/testevents"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// ParseEvents converts go test's JSON event stream into the shared test report.
func ParseEvents(r io.Reader) (verify.Report, []byte, error) { return testevents.ParseEvents(r) }

// ReportToSandboxResult converts the report to the shared Sandbox result contract.
func ReportToSandboxResult(report verify.Report, logRef workflow.BlobRef) ports.SandboxResult {
	return testevents.ReportToSandboxResult(report, logRef)
}
