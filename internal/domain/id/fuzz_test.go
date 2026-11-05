package id_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// FuzzParseChangeID checks that an accepted change id round trips through its text form and through JSON.
func FuzzParseChangeID(f *testing.F) {
	for _, s := range []string{"PAY-142", "pr-acme-pay-demo-7", "pr-a--b-c-1", "AB_C-12"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		c, err := id.ParseChangeID(s)
		if err != nil {
			return
		}
		again, err := id.ParseChangeID(c.String())
		if err != nil || again != c {
			t.Fatalf("re-parse of %q gave %q, %v", s, again, err)
		}
		if c.IsPR() {
			repo, n, err := c.PullRequest()
			if err != nil {
				return
			}
			back, err := id.PRChangeID(repo, n)
			if err != nil || back != c {
				t.Fatalf("PullRequest(%q) = %q, %d, which maps back to %q, %v", s, repo, n, back, err)
			}
		}
		var v id.ChangeID
		if err := json.Unmarshal([]byte(`"`+s+`"`), &v); err == nil && v != c {
			t.Fatalf("JSON decode of %q gave %q, want %q", s, v, c)
		}
	})
}

// FuzzPullRequest checks that any accepted pr- id maps back to itself through PRChangeID.
func FuzzPullRequest(f *testing.F) {
	f.Add("pr-a--b-c-1")
	f.Add("pr-acme-pay-demo-7")
	f.Fuzz(func(t *testing.T, s string) {
		c, err := id.ParseChangeID(s)
		if err != nil {
			return
		}
		repo, n, err := c.PullRequest()
		if err != nil {
			return
		}
		back, err := id.PRChangeID(repo, n)
		if err != nil || back != c {
			t.Fatalf("%q -> %q, %d -> %q, %v", s, repo, n, back, err)
		}
	})
}

// FuzzPRChangeID checks that the repository mapping is reversible for any accepted repository and number.
func FuzzPRChangeID(f *testing.F) {
	f.Add("acme/pay-demo", 7)
	f.Add("a-b/c", 1)
	f.Fuzz(func(t *testing.T, repo string, n int) {
		c, err := id.PRChangeID(repo, n)
		if err != nil {
			return
		}
		got, num, err := c.PullRequest()
		if err != nil {
			t.Fatalf("PRChangeID(%q, %d) = %q, which PullRequest rejects: %v", repo, n, c, err)
		}
		if num != n {
			t.Fatalf("number %d came back as %d", n, num)
		}
		if got != repo && got != lowerASCII(repo) {
			t.Fatalf("repository %q came back as %q", repo, got)
		}
	})
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// FuzzParseSHA checks that an accepted hash keeps its text exactly.
func FuzzParseSHA(f *testing.F) {
	f.Add("9f1c2ab")
	f.Add("9f1c2ab0c3d4e5f60718293a4b5c6d7e8f901234")
	f.Fuzz(func(t *testing.T, s string) {
		v, err := id.ParseSHA(s)
		if err != nil {
			return
		}
		if v.String() != s {
			t.Fatalf("%q came back as %q", s, v)
		}
		var back id.SHA
		if err := json.Unmarshal([]byte(`"`+s+`"`), &back); err != nil || back != v {
			t.Fatalf("JSON decode of %q: %q, %v", s, back, err)
		}
	})
}

// FuzzParseAgentName checks that an accepted name keeps its text exactly.
func FuzzParseAgentName(f *testing.F) {
	f.Add("bob-reviewer")
	f.Fuzz(func(t *testing.T, s string) {
		v, err := id.ParseAgentName(s)
		if err != nil {
			return
		}
		if v.String() != s {
			t.Fatalf("%q came back as %q", s, v)
		}
	})
}

// FuzzParsePrefixedIDs checks the four prefixed id types, including the ULID inside them.
func FuzzParsePrefixedIDs(f *testing.F) {
	f.Add("fnd_" + ulid)
	f.Add("run_" + ulid)
	f.Add("case_" + ulid)
	f.Add("tsk_" + ulid)
	f.Fuzz(func(t *testing.T, s string) {
		if v, err := id.ParseFindingID(s); err == nil && v.String() != s {
			t.Fatalf("finding %q came back as %q", s, v)
		}
		if v, err := id.ParseRunID(s); err == nil && v.String() != s {
			t.Fatalf("run %q came back as %q", s, v)
		}
		if v, err := id.ParseCaseID(s); err == nil && v.String() != s {
			t.Fatalf("case %q came back as %q", s, v)
		}
		if v, err := id.ParseTaskID(s); err == nil && v.String() != s {
			t.Fatalf("task %q came back as %q", s, v)
		}
	})
}

// FuzzParseULID checks that an accepted ULID has a time and that rebuilding it from the time and entropy gives the
// same text.
func FuzzParseULID(f *testing.F) {
	f.Add(ulid)
	f.Add("01ARYZ6S41TSV4RRFFQ69G5FAV")
	f.Fuzz(func(t *testing.T, s string) {
		u, err := id.ParseULID(s)
		if err != nil {
			return
		}
		ms, err := u.Time()
		if err != nil {
			t.Fatalf("accepted %q but Time failed: %v", s, err)
		}
		rebuilt, err := id.NewULID(ms, entropyOf(t, u))
		if err != nil || rebuilt != u {
			t.Fatalf("rebuild of %q gave %q, %v", s, rebuilt, err)
		}
	})
}

// entropyOf recovers the 80 random bits of u, one 5-bit character at a time, from the text itself.
func entropyOf(t *testing.T, u id.ULID) [10]byte {
	t.Helper()
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	var bits []int
	for i := 10; i < 26; i++ {
		v := strings.IndexByte(alphabet, u.String()[i])
		for b := 4; b >= 0; b-- {
			bits = append(bits, (v>>b)&1)
		}
	}
	var e [10]byte
	for i := range e {
		for b := 0; b < 8; b++ {
			e[i] |= byte(bits[i*8+b]) << (7 - b)
		}
	}
	return e
}

// FuzzULIDNext checks that Next always returns a greater id, or an error, and never panics.
func FuzzULIDNext(f *testing.F) {
	f.Add(ulid, uint64(1000))
	f.Fuzz(func(t *testing.T, s string, now uint64) {
		prev, err := id.ParseULID(s)
		if err != nil {
			return
		}
		next, err := id.Next(prev, now, [10]byte{})
		if err != nil {
			return
		}
		if next.String() <= prev.String() {
			t.Fatalf("Next(%q) = %q is not greater", prev, next)
		}
	})
}

// FuzzParseFindingRef checks that an accepted finding name is F plus its number, and that it marshals back.
func FuzzParseFindingRef(f *testing.F) {
	f.Add("F3")
	f.Add("F9999")
	f.Fuzz(func(t *testing.T, s string) {
		r, err := id.ParseFindingRef(s)
		if err != nil {
			return
		}
		if r.String() != s {
			t.Fatalf("%q came back as %q", s, r)
		}
		b, err := r.MarshalText()
		if err != nil {
			t.Fatalf("accepted %q but MarshalText failed: %v", s, err)
		}
		var back id.FindingRef
		if err := back.UnmarshalText(b); err != nil || back != r {
			t.Fatalf("marshal of %q did not unmarshal: %v", s, err)
		}
	})
}
