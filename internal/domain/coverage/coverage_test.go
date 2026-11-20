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
		name    string
		res     Result
		changed map[string][]int
		want    DiffReport
	}{
		{
			name: "covered, uncovered and not instrumented",
			res:  res,
			changed: map[string][]int{
				"internal/domain/coverage/coverage.go": {41, 42, 43, 44, 45},
				"internal/domain/coverage/lcov.go":     {1, 2},
				"docs/readme.md":                       {1},
			},
			want: DiffReport{
				Files: []FileDiff{
					{Path: "docs/readme.md", Counts: Counts{NotInstrumented: 1}},
					{Path: "internal/domain/coverage/coverage.go", Counts: Counts{Covered: 3, Uncovered: 1, NotInstrumented: 1}},
					{Path: "internal/domain/coverage/lcov.go", Counts: Counts{Covered: 1, Uncovered: 1}},
				},
				Total:   Counts{Covered: 4, Uncovered: 2, NotInstrumented: 2},
				Percent: 400.0 / 6,
			},
		},
		{
			name:    "nothing instrumented is 100 percent",
			res:     res,
			changed: map[string][]int{"x.go": {1, 2}},
			want: DiffReport{
				Files:   []FileDiff{{Path: "x.go", Counts: Counts{NotInstrumented: 2}}},
				Total:   Counts{NotInstrumented: 2},
				Percent: 100,
			},
		},
		{
			name:    "no changed lines",
			res:     res,
			changed: nil,
			want:    DiffReport{Files: []FileDiff{}, Percent: 100},
		},
		{
			name:    "all instrumented lines uncovered",
			res:     res,
			changed: map[string][]int{"internal/domain/coverage/lcov.go": {2}},
			want: DiffReport{
				Files: []FileDiff{
					{Path: "internal/domain/coverage/lcov.go", Counts: Counts{Uncovered: 1}},
				},
				Total:   Counts{Uncovered: 1},
				Percent: 0,
			},
		},
		{
			name:    "a duplicate changed line counts once",
			res:     res,
			changed: map[string][]int{"internal/domain/coverage/lcov.go": {1, 1, 1}},
			want: DiffReport{
				Files: []FileDiff{
					{Path: "internal/domain/coverage/lcov.go", Counts: Counts{Covered: 1}},
				},
				Total:   Counts{Covered: 1},
				Percent: 100,
			},
		},
		{
			name:    "changed paths are normalized",
			res:     res,
			changed: map[string][]int{"./internal\\domain\\coverage\\lcov.go": {1}},
			want: DiffReport{
				Files: []FileDiff{
					{Path: "internal/domain/coverage/lcov.go", Counts: Counts{Covered: 1}},
				},
				Total:   Counts{Covered: 1},
				Percent: 100,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Diff(c.res, c.changed)
			require.NoError(t, err)
			require.Equal(t, c.want.Files, got.Files)
			require.Equal(t, c.want.Total, got.Total)
			require.InDelta(t, c.want.Percent, got.Percent, 1e-9)
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
