package change_test

import (
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
)

func TestFindingRecordAcceptsAWholeReproTestFile(t *testing.T) {
	check := func(content string) string {
		rec := change.FindingRecord{ReproTest: &finding.ReproTest{Path: "tests/x_test.go", Name: "TestX", Content: content}}
		err := rec.Validate()
		if err == nil {
			return ""
		}
		return err.Error()
	}
	if got := check(strings.Repeat("a", 4000)); strings.Contains(got, "repro_test.content") {
		t.Errorf("a 4000 byte test file is refused: %s", got)
	}
	if got := check(strings.Repeat("a", change.MaxTestSourceBytes+1)); !strings.Contains(got, "repro_test.content") {
		t.Errorf("an oversized test file is accepted: %s", got)
	}
}
