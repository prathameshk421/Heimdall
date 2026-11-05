package criteria

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func digest(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func ids(cs []ports.Criterion) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func texts(cs []ports.Criterion) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Text)
	}
	return out
}

func TestNumberedItemsUnderTheHeadingBecomeCriteriaInOrder(t *testing.T) {
	text := "# PAY-142: Add refunds\n\nMerchants can refund a payment.\n\n## Acceptance criteria\n\n" +
		"1. A refund cannot exceed the original payment.\n" +
		"2. A repeated request returns the original refund.\n" +
		"3. Only the merchant can refund.\n"

	got, err := Parse(text)

	require.NoError(t, err)
	require.Equal(t, []string{"AC1", "AC2", "AC3"}, ids(got))
	require.Equal(t, "A refund cannot exceed the original payment.", got[0].Text)
	require.Equal(t, digest(got[0].Text), got[0].Hash, "the hash is the SHA-256 of the criterion text")
}

func TestBulletMarkersAllCountAsItems(t *testing.T) {
	got, err := Parse("## Acceptance criteria\n- alpha\n* beta\n+ gamma\n")

	require.NoError(t, err)
	require.Equal(t, []string{"alpha", "beta", "gamma"}, texts(got))
}

func TestTheHeadingMatchesAnyLevelAndCaseWithAnOptionalColon(t *testing.T) {
	for _, heading := range []string{"## Acceptance criteria", "### acceptance criteria:", "# ACCEPTANCE CRITERIA", "## Acceptance Criteria :"} {
		got, err := Parse(heading + "\n- one\n")
		require.NoError(t, err, heading)
		require.Equal(t, []string{"one"}, texts(got), heading)
	}
}

func TestTheSectionEndsAtTheNextHeading(t *testing.T) {
	got, err := Parse("## Acceptance criteria\n- counted\n\n## Notes\n- not a criterion\n")

	require.NoError(t, err)
	require.Equal(t, []string{"counted"}, texts(got))
}

func TestNoHeadingMeansNoCriteriaAndNoError(t *testing.T) {
	got, err := Parse("# PAY-1: x\n\n- a bullet that is not under the heading\n")

	require.NoError(t, err)
	require.Empty(t, got)
}

func TestNestedItemsAndWrappedLinesJoinTheirParent(t *testing.T) {
	text := "## Acceptance criteria\n- first line\n  - nested detail\n  wrapped text\n- second\n"

	got, err := Parse(text)

	require.NoError(t, err)
	require.Equal(t, []string{"first line\nnested detail\nwrapped text", "second"}, texts(got))
}

func TestABlankLineEndsTheCurrentItem(t *testing.T) {
	got, err := Parse("## Acceptance criteria\n- one\n\nnot an item\n- two\n")

	require.NoError(t, err)
	require.Equal(t, []string{"one", "two"}, texts(got))
}

func TestSeveralAcceptanceSectionsNumberOnAcrossThem(t *testing.T) {
	got, err := Parse("## Acceptance criteria\n- a\n## Other\n## Acceptance criteria\n- b\n")

	require.NoError(t, err)
	require.Equal(t, []string{"AC1", "AC2"}, ids(got))
	require.Equal(t, []string{"a", "b"}, texts(got))
}

func TestTheHashFollowsTheTextNotTheSurroundings(t *testing.T) {
	a, err := Parse("## Acceptance criteria\n- same text\n")
	require.NoError(t, err)
	b, err := Parse("# Other\n## Acceptance criteria\n\n1. same text\n")
	require.NoError(t, err)

	require.Len(t, a, 1)
	require.Len(t, b, 1)
	require.Equal(t, a[0].Hash, b[0].Hash, "an edit that keeps the text keeps the hash (D50)")
	require.Equal(t, a[0].ID, b[0].ID)
}

func TestAChangedCriterionGetsADifferentHash(t *testing.T) {
	a, err := Parse("## Acceptance criteria\n- refunds are capped\n")
	require.NoError(t, err)
	b, err := Parse("## Acceptance criteria\n- refunds are capped at 100\n")
	require.NoError(t, err)

	require.Len(t, a, 1)
	require.Len(t, b, 1)
	require.NotEqual(t, a[0].Hash, b[0].Hash)
}

func TestTheTextIsTrimmed(t *testing.T) {
	got, err := Parse("## Acceptance criteria\n-    padded text   \n")

	require.NoError(t, err)
	require.Equal(t, []string{"padded text"}, texts(got))
	require.Equal(t, digest("padded text"), got[0].Hash)
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
