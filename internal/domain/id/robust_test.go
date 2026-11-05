package id_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

func TestULIDFirstCharacterFitsFortyEightBits(t *testing.T) {
	_, err := id.ParseFindingID("fnd_80000000000000000000000000")
	require.Error(t, err, "a first character of 8 or more overflows the 48-bit timestamp")
	_, err = id.ParseFindingID("fnd_ZZZZZZZZZZZZZZZZZZZZZZZZZZ")
	require.Error(t, err)
	_, err = id.ParseFindingID("fnd_7ZZZZZZZZZZZZZZZZZZZZZZZZZ")
	require.NoError(t, err, "the largest timestamp is 7ZZZZZZZZZ")
}

func TestPRChangeIDDoesNotCollideAcrossSlashAndHyphen(t *testing.T) {
	a, err := id.PRChangeID("a/b-c", 1)
	require.NoError(t, err)
	b, err := id.PRChangeID("a-b/c", 1)
	require.NoError(t, err)
	require.NotEqual(t, a.String(), b.String(), "a/b-c and a-b/c must be different streams")
	require.Equal(t, "pr-a-b-c-1", a.String())
}

func TestPRChangeIDRoundTrips(t *testing.T) {
	for _, tc := range []struct {
		repo   string
		number int
	}{
		{"acme/pay-demo", 7},
		{"a/b-c", 1},
		{"a-b/c", 2},
		{"a--b/c-", 3},
		{"aarush-linux/x.y_z", 4},
		{"acme/pay-7", 12},
	} {
		c, err := id.PRChangeID(tc.repo, tc.number)
		require.NoError(t, err, tc.repo)
		repo, n, err := c.PullRequest()
		require.NoError(t, err, c.String())
		require.Equal(t, tc.repo, repo)
		require.Equal(t, tc.number, n)
	}
}

func TestPullRequestRejectsTicketKeys(t *testing.T) {
	k, err := id.ParseChangeID("PAY-142")
	require.NoError(t, err)
	_, _, err = k.PullRequest()
	require.Error(t, err)
}

func TestTicketKeysMayHoldDigitsAndUnderscores(t *testing.T) {
	for _, ok := range []string{"AB_C-12", "A1-1", "AB1_2-99", "PAY_2-1"} {
		_, err := id.ParseChangeID(ok)
		require.NoError(t, err, ok)
	}
	for _, bad := range []string{"ab_c-12", "-12", "AB_C-", "PAY-", "_AB-1", "1AB-1", "AB-C-1"} {
		_, err := id.ParseChangeID(bad)
		require.Error(t, err, "should reject %q", bad)
	}
}

func TestNewFindingRefRejectsNonPositiveAndOverflow(t *testing.T) {
	for _, bad := range []int{0, -1, 10000} {
		_, err := id.NewFindingRef(bad)
		require.Error(t, err, "should reject %d", bad)
	}
	r, err := id.NewFindingRef(9999)
	require.NoError(t, err)
	require.Equal(t, "F9999", r.String())
}

func TestFindingRefMarshalRoundTrip(t *testing.T) {
	r, err := id.NewFindingRef(42)
	require.NoError(t, err)
	b, err := r.MarshalText()
	require.NoError(t, err)
	var back id.FindingRef
	require.NoError(t, back.UnmarshalText(b))
	require.Equal(t, r, back)
	_, err = id.FindingRef(0).MarshalText()
	require.Error(t, err, "a value that cannot unmarshal must not marshal")
	_, err = id.FindingRef(-3).MarshalText()
	require.Error(t, err)
}

func TestUnmarshalTextDoesNotOverwriteOnError(t *testing.T) {
	c, err := id.ParseChangeID("PAY-142")
	require.NoError(t, err)
	require.Error(t, c.UnmarshalText([]byte("nope")))
	require.Equal(t, "PAY-142", c.String())

	s, err := id.ParseSHA("9f1c2ab")
	require.NoError(t, err)
	require.Error(t, s.UnmarshalText([]byte("zz")))
	require.Equal(t, "9f1c2ab", s.String())

	a, err := id.ParseAgentName("bob")
	require.NoError(t, err)
	require.Error(t, a.UnmarshalText([]byte("Bad")))
	require.Equal(t, "bob", a.String())

	f, err := id.ParseFindingID("fnd_" + ulid)
	require.NoError(t, err)
	require.Error(t, f.UnmarshalText([]byte("x")))
	require.Equal(t, "fnd_"+ulid, f.String())

	r, err := id.ParseFindingRef("F7")
	require.NoError(t, err)
	require.Error(t, r.UnmarshalText([]byte("F0")))
	require.Equal(t, 7, r.Number())
}

func TestValidReportsEveryTypeAndZeroValues(t *testing.T) {
	require.True(t, id.ChangeID("PAY-142").Valid())
	require.False(t, id.ChangeID("pay-142").Valid())
	require.True(t, id.SHA("9f1c2ab").Valid())
	require.False(t, id.SHA("").Valid())
	require.True(t, id.AgentName("bob").Valid())
	require.False(t, id.AgentName("").Valid())
	require.True(t, id.FindingID("fnd_"+ulid).Valid())
	require.False(t, id.FindingID("").Valid())
	require.True(t, id.RunID("run_"+ulid).Valid())
	require.False(t, id.RunID("fnd_"+ulid).Valid())
	require.True(t, id.CaseID("case_"+ulid).Valid())
	require.False(t, id.CaseID("").Valid())
	require.True(t, id.TaskID("tsk_"+ulid).Valid())
	require.False(t, id.TaskID("").Valid())
	require.True(t, id.FindingRef(3).Valid())
	require.False(t, id.FindingRef(0).Valid())
}

func TestULIDSpecKnownAnswer(t *testing.T) {
	entropy := [10]byte{0xd6, 0x76, 0x4c, 0x61, 0xef, 0xb9, 0x93, 0x02, 0xbd, 0x5b}
	u, err := id.NewULID(1469918176385, entropy)
	require.NoError(t, err)
	require.Equal(t, "01ARYZ6S41TSV4RRFFQ69G5FAV", u.String(), "ULID specification example")

	zero, err := id.NewULID(1469918176385, [10]byte{})
	require.NoError(t, err)
	require.Equal(t, "01ARYZ6S410000000000000000", zero.String())
}

func TestULIDTimeExtraction(t *testing.T) {
	u, err := id.ParseULID("01ARYZ6S41TSV4RRFFQ69G5FAV")
	require.NoError(t, err)
	ms, err := u.Time()
	require.NoError(t, err)
	require.Equal(t, uint64(1469918176385), ms)

	top, err := id.NewULID(1<<48-1, [10]byte{})
	require.NoError(t, err)
	require.Equal(t, "7ZZZZZZZZZ0000000000000000", top.String())
	ms, err = top.Time()
	require.NoError(t, err)
	require.Equal(t, uint64(1<<48-1), ms)

	_, err = id.NewULID(1<<48, [10]byte{})
	require.Error(t, err, "a timestamp beyond 48 bits must not be truncated")
}

func TestULIDStringOrderMatchesTimeThenEntropy(t *testing.T) {
	type pair struct {
		ms uint64
		e  [10]byte
	}
	ordered := []pair{
		{0, [10]byte{}},
		{0, [10]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 1}},
		{0, [10]byte{0, 0, 0, 0, 0, 0, 0, 0, 1, 0}},
		{0, [10]byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0}},
		{0, [10]byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 255}},
		{1, [10]byte{}},
		{1469918176385, [10]byte{0xd6, 0x76}},
		{1469918176386, [10]byte{}},
		{1<<48 - 1, [10]byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 254}},
	}
	var prev string
	for i, p := range ordered {
		u, err := id.NewULID(p.ms, p.e)
		require.NoError(t, err)
		if i > 0 {
			require.Less(t, prev, u.String(), "pair %d must sort after pair %d", i, i-1)
		}
		prev = u.String()
	}
}

func TestNextIsStrictlyGreaterWithinOneMillisecond(t *testing.T) {
	base := [10]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff}
	first, err := id.NewULID(1000, base)
	require.NoError(t, err)
	second, err := id.Next(first, 1000, [10]byte{})
	require.NoError(t, err)
	require.Less(t, first.String(), second.String())
	third, err := id.Next(second, 999, [10]byte{})
	require.NoError(t, err, "a clock that steps back must still give a greater id")
	require.Less(t, second.String(), third.String())

	carry := [10]byte{0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff}
	c1, err := id.NewULID(1000, carry)
	require.NoError(t, err)
	c2, err := id.Next(c1, 1000, [10]byte{})
	require.NoError(t, err)
	require.Less(t, c1.String(), c2.String(), "a carry out of the low byte must still increase the id")
	ms, err := c2.Time()
	require.NoError(t, err)
	require.Equal(t, uint64(1000), ms, "Next keeps the previous timestamp when the clock has not moved on")
}

func TestNextUsesNewEntropyWhenTheClockMovedOn(t *testing.T) {
	prev, err := id.NewULID(1000, [10]byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 255})
	require.NoError(t, err)
	next, err := id.Next(prev, 1001, [10]byte{})
	require.NoError(t, err)
	require.Less(t, prev.String(), next.String())
	ms, err := next.Time()
	require.NoError(t, err)
	require.Equal(t, uint64(1001), ms)
}

func TestNextReportsRandomOverflow(t *testing.T) {
	prev, err := id.NewULID(1000, [10]byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 255})
	require.NoError(t, err)
	_, err = id.Next(prev, 1000, [10]byte{})
	require.Error(t, err, "incrementing an exhausted random part must fail, not wrap")
}

func TestNextRejectsInvalidPrevious(t *testing.T) {
	_, err := id.Next("not-a-ulid", 1000, [10]byte{})
	require.Error(t, err)
}

func TestPrefixedIDsRejectOverflowingTimestampInJSON(t *testing.T) {
	var v struct {
		F id.FindingID `json:"f"`
	}
	require.Error(t, json.Unmarshal([]byte(`{"f":"fnd_80000000000000000000000000"}`), &v))
	require.False(t, strings.HasPrefix(v.F.String(), "fnd_8"))
}
