package policy_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

func seated(t *testing.T) policy.Policy {
	t.Helper()
	p := freshPolicy(t)
	p.Seats = map[string]policy.Seat{}
	for _, person := range p.People {
		if person.Seat != "" {
			p.Seats[person.Seat] = policy.Seat{Role: "dev", Lenses: []string{"correctness"}}
		}
	}
	p.Seats["bob-reviewer"] = policy.Seat{Role: "dev", Lenses: []string{"correctness", "contract"}}
	p.Seats["eli-reviewer"] = policy.Seat{Role: "security", Lenses: []string{"security"}}
	return p
}

// REVIEW_ARCH 5.3 and BUILD_PLAN M0 step 7: the seat rules the policy file can check, and the ones that need the
// repository's adopted lenses and the deployment's agents.
func TestSeatValidation(t *testing.T) {
	require.NoError(t, seated(t).Validate())

	for name, tc := range map[string]struct {
		mutate func(*policy.Policy)
		want   string
	}{
		"a person on a seat that is not declared": {func(p *policy.Policy) {
			p.People["zed"] = policy.Person{Seat: "zed-reviewer"}
		}, `people.zed.seat: "zed-reviewer" is not a seat in seats`},
		"two people on one seat": {func(p *policy.Policy) {
			p.People["zed"] = policy.Person{Seat: "bob-reviewer"}
		}, "people.zed.seat"},
		"a seat with no lens":       {func(p *policy.Policy) { p.Seats["bob-reviewer"] = policy.Seat{Role: "dev"} }, "seats.bob-reviewer.lenses"},
		"a lens that is not a name": {func(p *policy.Policy) { p.Seats["bob-reviewer"] = policy.Seat{Role: "dev", Lenses: []string{"Data!"}} }, "seats.bob-reviewer.lenses"},
		"a role that is not a name": {func(p *policy.Policy) { p.Seats["bob-reviewer"] = policy.Seat{Role: "", Lenses: []string{"data"}} }, "seats.bob-reviewer.role"},
	} {
		t.Run(name, func(t *testing.T) {
			p := seated(t)
			tc.mutate(&p)
			require.ErrorContains(t, p.Validate(), tc.want)
		})
	}

	var agents []string
	for seat := range seated(t).Seats {
		agents = append(agents, seat)
	}
	adopted := []string{"correctness", "contract", "security"}
	require.NoError(t, policy.ValidateSeats(seated(t), agents, adopted))
	require.ErrorContains(t, policy.ValidateSeats(seated(t), []string{"bob-reviewer"}, adopted), "seats.eli-reviewer: no such agent")
	require.NotContains(t, policy.ValidateSeats(seated(t), []string{"bob-reviewer"}, adopted).Error(), "seats.bob-reviewer: no such agent")
	require.ErrorContains(t, policy.ValidateSeats(seated(t), agents, []string{"correctness", "contract"}), `"security" is not an adopted lens`)
	require.ErrorContains(t, policy.ValidateSeats(seated(t), agents, append(adopted, "data")), `lens "data" is adopted but no seat holds it`)
}
