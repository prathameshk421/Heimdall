package coverage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiff(t *testing.T) {
	res := Result{Files: map[string]Lines{
		"internal/domain/coverage/coverage.go": {41: 12, 42: 12, 43: 11, 44: 0},
		"internal/domain/coverage/lcov.go":     {1: 1, 2: 0},
	}}
	cases := []struct {
		name       string
		res        Result
		changed    map[string][]int
		want       DiffReport
		measurable bool
	}{
		{
			name: "covered, uncovered and not instrumented",
			res:  res,
			changed: map[string][]int{
				"internal/domain/coverage/coverage.go": {41, 42, 43, 44, 45},
				"internal/domain/coverage/lcov.go":     {1, 2},
			},
			want: DiffReport{
				Files: []FileDiff{
					{Path: "internal/domain/coverage/coverage.go", Counts: Counts{Covered: 3, Uncovered: 1, NotInstrumented: 1}},
					{Path: "internal/domain/coverage/lcov.go", Counts: Counts{Covered: 1, Uncovered: 1}},
				},
				Total:        Counts{Covered: 4, Uncovered: 2, NotInstrumented: 1},
				ChangedLines: 7,
				Percent:      400.0 / 6,
			},
			measurable: true,
		},
		{
			name:    "a file the report never names is missing",
			res:     res,
			changed: map[string][]int{"x.go": {1, 2}},
			want: DiffReport{
				Files:        []FileDiff{{Path: "x.go", Missing: true}},
				ChangedLines: 2,
				MissingFiles: 1,
				Percent:      100,
			},
			measurable: false,
		},
		{
			name:    "a wrong prefix makes every changed file missing",
			res:     res,
			changed: map[string][]int{"coverage.go": {41}, "lcov.go": {1}},
			want: DiffReport{
				Files: []FileDiff{
					{Path: "coverage.go", Missing: true},
					{Path: "lcov.go", Missing: true},
				},
				ChangedLines: 2,
				MissingFiles: 2,
				Percent:      100,
			},
			measurable: false,
		},
		{
			name:    "a missing file is not counted as not instrumented",
			res:     res,
			changed: map[string][]int{"x.go": {1, 2}, "internal/domain/coverage/lcov.go": {1}},
			want: DiffReport{
				Files: []FileDiff{
					{Path: "internal/domain/coverage/lcov.go", Counts: Counts{Covered: 1}},
					{Path: "x.go", Missing: true},
				},
				Total:        Counts{Covered: 1},
				ChangedLines: 3,
				MissingFiles: 1,
				Percent:      100,
			},
			measurable: false,
		},
		{
			name:    "a file the caller should have left out is still missing",
			res:     res,
			changed: map[string][]int{"docs/readme.md": {1}, "internal/domain/coverage/lcov.go": {1}},
			want: DiffReport{
				Files: []FileDiff{
					{Path: "docs/readme.md", Missing: true},
					{Path: "internal/domain/coverage/lcov.go", Counts: Counts{Covered: 1}},
				},
				Total:        Counts{Covered: 1},
				ChangedLines: 2,
				MissingFiles: 1,
				Percent:      100,
			},
			measurable: false,
		},
		{
			name:    "a named file with only non-instrumented changed lines",
			res:     res,
			changed: map[string][]int{"internal/domain/coverage/lcov.go": {3, 4}},
			want: DiffReport{
				Files: []FileDiff{
					{Path: "internal/domain/coverage/lcov.go", Counts: Counts{NotInstrumented: 2}},
				},
				Total:        Counts{NotInstrumented: 2},
				ChangedLines: 2,
				Percent:      100,
			},
			measurable: false,
		},
		{
			name:    "a named file with an empty changed line list is measurable",
			res:     res,
			changed: map[string][]int{"internal/domain/coverage/lcov.go": nil},
			want: DiffReport{
				Files:   []FileDiff{{Path: "internal/domain/coverage/lcov.go"}},
				Percent: 100,
			},
			measurable: true,
		},
		{
			name:    "no changed lines",
			res:     res,
			changed: nil,
			want:    DiffReport{Files: []FileDiff{}, Percent: 100},
			// Nothing was given, so nothing is missing: there is nothing to measure.
			measurable: true,
		},
		{
			name:    "all instrumented lines uncovered",
			res:     res,
			changed: map[string][]int{"internal/domain/coverage/lcov.go": {2}},
			want: DiffReport{
				Files: []FileDiff{
					{Path: "internal/domain/coverage/lcov.go", Counts: Counts{Uncovered: 1}},
				},
				Total:        Counts{Uncovered: 1},
				ChangedLines: 1,
				Percent:      0,
			},
			measurable: true,
		},
		{
			name:    "a duplicate changed line counts once",
			res:     res,
			changed: map[string][]int{"internal/domain/coverage/lcov.go": {1, 1, 1}},
			want: DiffReport{
				Files: []FileDiff{
					{Path: "internal/domain/coverage/lcov.go", Counts: Counts{Covered: 1}},
				},
				Total:        Counts{Covered: 1},
				ChangedLines: 1,
				Percent:      100,
			},
			measurable: true,
		},
		{
			name:    "changed paths are normalized",
			res:     res,
			changed: map[string][]int{"./internal\\domain\\coverage\\lcov.go": {1}},
			want: DiffReport{
				Files: []FileDiff{
					{Path: "internal/domain/coverage/lcov.go", Counts: Counts{Covered: 1}},
				},
				Total:        Counts{Covered: 1},
				ChangedLines: 1,
				Percent:      100,
			},
			measurable: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Diff(c.res, c.changed)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
			require.Equal(t, c.measurable, got.Measurable())
		})
	}
}

func TestDiffRefusesBadChangedLines(t *testing.T) {
	cases := []struct {
		name    string
		changed map[string][]int
	}{
		{"line zero", map[string][]int{"a.go": {0}}},
		{"negative line", map[string][]int{"a.go": {-1}}},
		{"line above the cap", map[string][]int{"a.go": {MaxLine + 1}}},
		{"path escapes the root", map[string][]int{"../a.go": {1}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Diff(Result{}, c.changed)
			require.Error(t, err)
		})
	}
}

func TestCountsPercent(t *testing.T) {
	cases := []struct {
		name  string
		count Counts
		want  float64
	}{
		{"nothing instrumented", Counts{NotInstrumented: 9}, 100},
		{"one covered of two", Counts{Covered: 1, Uncovered: 1}, 50},
		{"two covered of three", Counts{Covered: 2, Uncovered: 1}, 200.0 / 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.InDelta(t, c.want, c.count.Percent(), 1e-9)
		})
	}
}

func TestBuilderRefusesBadValues(t *testing.T) {
	b, err := NewBuilder(Options{MaxBytes: 1 << 20})
	require.NoError(t, err)
	cases := []struct {
		name string
		line int
		hits int
	}{
		{"line zero", 0, 1},
		{"line above the cap", MaxLine + 1, 1},
		{"negative hits", 1, -1},
		{"hits above the cap", 1, maxHits + 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.ErrorIs(t, b.Add(1, "a.go", c.line, c.hits), ErrMalformed)
		})
	}
}

func TestBuilderAppliesPathRules(t *testing.T) {
	b, err := NewBuilder(Options{MaxBytes: 1 << 20, Prefix: "/home/runner/work/app"})
	require.NoError(t, err)
	require.NoError(t, b.Add(1, `/home/runner/work/app/app\service.py`, 1, 2))
	require.NoError(t, b.Add(2, "./app//service.py", 1, 3))
	// A prefix is removed once: the rebased path is not rebased again.
	require.NoError(t, b.Add(3, "/home/runner/work/app/home/runner/work/app/x.py", 7, 0))
	require.Equal(t, Result{Files: map[string]Lines{
		"app/service.py":            {1: 5},
		"home/runner/work/app/x.py": {7: 0},
	}}, b.Result())
}

func TestBuilderRefusesBadPaths(t *testing.T) {
	b, err := NewBuilder(Options{MaxBytes: 1 << 20})
	require.NoError(t, err)
	for _, raw := range []string{"../a.go", "", "."} {
		require.ErrorIs(t, b.Add(4, raw, 1, 1), ErrMalformed, raw)
		err := b.CheckFile(4, raw)
		require.ErrorIs(t, err, ErrMalformed, raw)
		require.Contains(t, err.Error(), "line 4:")
	}
	require.NoError(t, b.CheckFile(1, "a.go"))
	require.Empty(t, b.Result().Files)
}

func TestBuilderRefusesBadOptions(t *testing.T) {
	_, err := NewBuilder(Options{})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrMalformed)
	_, err = NewBuilder(Options{MaxBytes: 1, Prefix: "../app"})
	require.ErrorContains(t, err, "prefix")
}

func TestBuilderSizeLimit(t *testing.T) {
	b, err := NewBuilder(Options{MaxBytes: 3})
	require.NoError(t, err)
	require.NoError(t, b.CheckSize([]byte("abc")))
	require.ErrorIs(t, b.CheckSize([]byte("abcd")), ErrTooLarge)
}

func TestBuilderStopsAtMaxLines(t *testing.T) {
	b, err := NewBuilder(Options{MaxBytes: 1})
	require.NoError(t, err)
	b.lines = MaxLines
	require.ErrorIs(t, b.Add(1, "a.go", 1, 1), ErrTooLarge)
}
