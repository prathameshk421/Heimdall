package criteria

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func texts(cs []ports.Criterion) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Text)
	}
	return out
}

func TestTheSectionEndsAtTheNextHeading(t *testing.T) {
	got, err := Parse("## Acceptance criteria\n- counted\n\n## Notes\n- not a criterion\n")

	require.NoError(t, err)
	require.Equal(t, []string{"counted"}, texts(got))
}

func TestNestedItemsAndWrappedLinesJoinTheirParent(t *testing.T) {
	text := "## Acceptance criteria\n- first line\n  - nested detail\n  wrapped text\n- second\n"

	got, err := Parse(text)

	require.NoError(t, err)
	require.Equal(t, []string{"first line\nnested detail\nwrapped text", "second"}, texts(got))
}

func TestNineHundredNinetyNineCriteriaAreAllowedAndAThousandAreNot(t *testing.T) {
	var ok, tooMany strings.Builder
	ok.WriteString("## Acceptance criteria\n")
	tooMany.WriteString("## Acceptance criteria\n")
	for i := 1; i <= 1000; i++ {
		line := "- item " + strings.Repeat("x", 3) + "\n"
		tooMany.WriteString(line)
		if i <= 999 {
			ok.WriteString(line)
		}
	}

	got, err := Parse(ok.String())
	require.NoError(t, err)
	require.Len(t, got, 999)
	require.Equal(t, "AC999", got[998].ID)

	_, err = Parse(tooMany.String())
	require.Error(t, err, "AC1000 is not a valid id (the ticket model stops at AC999)")
}
