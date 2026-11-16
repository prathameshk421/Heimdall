package review

import (
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// Pick is a seat chosen for one change, with the lenses it is asked to cover and the person who lends it, if any.
type Pick struct {
	Seat   string   `json:"seat"`
	Lenses []string `json:"lenses"`
	Person string   `json:"person,omitempty"`
}

// NeededLenses returns the lenses this change needs, sorted: a lens whose when-globs match a changed file, a lens of a
// new tool result, and a lens that policy requires for a changed path (reviewers.required).
func NeededLenses(files []string, lensWhen map[string][]string, resultLenses []string, required map[string][]string) []string {
	need := map[string]bool{}
	for lens, globs := range lensWhen {
		for _, f := range files {
			if matchAny(globs, f) {
				need[lens] = true
			}
		}
	}
	for _, l := range resultLenses {
		need[l] = true
	}
	for glob, lenses := range required {
		for _, f := range files {
			if policy.MatchGlob(glob, f) {
				for _, l := range lenses {
					need[l] = true
				}
			}
		}
	}
	return sortedKeys(need)
}

func matchAny(globs []string, file string) bool {
	for _, g := range globs {
		if policy.MatchGlob(g, file) {
			return true
		}
	}
	return false
}

// PickSeats chooses seats for the needed lenses, at most count of them. Seats the author named go first, in the order
// given. Then seats that hold a still-uncovered needed lens are taken, the one covering the most first and ties by name.
// It returns the picks and the needed lenses no picked seat holds.
func PickSeats(needed []string, seats map[string]policy.Seat, people map[string]string, author []string, count int) ([]Pick, []string) {
	var picks []Pick
	taken := map[string]bool{}
	covered := map[string]bool{}
	take := func(name string) {
		s := seats[name]
		picks = append(picks, Pick{Seat: name, Lenses: intersect(s.Lenses, needed), Person: people[name]})
		taken[name] = true
		for _, l := range s.Lenses {
			covered[l] = true
		}
	}
	for _, name := range author {
		if _, ok := seats[name]; ok && !taken[name] && len(picks) < count {
			take(name)
		}
	}
	for len(picks) < count {
		next := bestSeat(seats, needed, covered, taken)
		if next == "" {
			break
		}
		take(next)
	}
	var uncovered []string
	for _, l := range needed {
		if !covered[l] {
			uncovered = append(uncovered, l)
		}
	}
	return picks, uncovered
}

// bestSeat is the untaken seat that covers the most still-uncovered needed lenses, ties by name. It is "" when no seat
// covers one.
func bestSeat(seats map[string]policy.Seat, needed []string, covered, taken map[string]bool) string {
	best, bestN := "", 0
	for _, name := range sortedSeatNames(seats) {
		if taken[name] {
			continue
		}
		if n := len(uncoveredIn(seats[name].Lenses, needed, covered)); n > bestN {
			best, bestN = name, n
		}
	}
	return best
}

func uncoveredIn(seatLenses, needed []string, covered map[string]bool) []string {
	var out []string
	for _, l := range seatLenses {
		if slices.Contains(needed, l) && !covered[l] {
			out = append(out, l)
		}
	}
	return out
}

func intersect(seatLenses, needed []string) []string {
	var out []string
	for _, l := range seatLenses {
		if slices.Contains(needed, l) {
			out = append(out, l)
		}
	}
	return out
}

func sortedSeatNames(seats map[string]policy.Seat) []string {
	names := make([]string, 0, len(seats))
	for n := range seats {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
