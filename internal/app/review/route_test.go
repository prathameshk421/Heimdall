package review

import (
	"reflect"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

var lensWhen = map[string][]string{
	"data":     {"**/migrations/**", "**/*.sql"},
	"infra":    {"deploy/**"},
	"security": {"**/*"},
}

func TestNeededLenses(t *testing.T) {
	cases := []struct {
		name    string
		files   []string
		results []string
		req     map[string][]string
		want    []string
	}{
		{"a migration needs data and security", []string{"migrations/0012.sql"}, nil, nil, []string{"data", "security"}},
		{"a deploy file needs infra and security", []string{"deploy/app.yaml"}, nil, nil, []string{"infra", "security"}},
		{"a new result adds its lens", []string{"main.go"}, []string{"contract"}, nil, []string{"contract", "security"}},
		{"policy requires a lens for a path", []string{"internal/auth/token.go"}, nil,
			map[string][]string{"internal/auth/**": {"security"}}, []string{"security"}},
		{"a required lens is added even if no pack matches", []string{"docs/x.md"}, nil,
			map[string][]string{"docs/**": {"tests"}}, []string{"security", "tests"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NeededLenses(c.files, lensWhen, c.results, c.req)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("NeededLenses = %v, want %v", got, c.want)
			}
		})
	}
}

var seats = map[string]policy.Seat{
	"reviewer-1": {Role: "dev", Lenses: []string{"correctness", "contract", "tests"}},
	"reviewer-2": {Role: "security", Lenses: []string{"security", "data"}},
	"reviewer-3": {Role: "infra", Lenses: []string{"infra", "operability"}},
}

func TestPickSeats(t *testing.T) {
	cases := []struct {
		name       string
		needed     []string
		author     []string
		count      int
		wantSeats  []string
		wantUnseen []string
	}{
		{"each needed lens reaches its holder", []string{"data", "security"}, nil, 3,
			[]string{"reviewer-2"}, nil},
		{"author picks come first", []string{"infra"}, []string{"reviewer-1"}, 3,
			[]string{"reviewer-1", "reviewer-3"}, nil},
		{"the count caps the picks; ties go to the earlier name; the rest are reported", []string{"data", "infra", "contract"}, nil, 1,
			[]string{"reviewer-1"}, []string{"data", "infra"}},
		{"a lens no seat holds is reported", []string{"ui"}, nil, 3, nil, []string{"ui"}},
		{"no needed lens picks nobody", nil, nil, 3, nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			picks, unseen := PickSeats(c.needed, seats, map[string]string{"reviewer-2": "priya"}, c.author, c.count)
			var names []string
			for _, p := range picks {
				names = append(names, p.Seat)
			}
			if !reflect.DeepEqual(names, c.wantSeats) {
				t.Fatalf("picked %v, want %v", names, c.wantSeats)
			}
			if !reflect.DeepEqual(unseen, c.wantUnseen) {
				t.Fatalf("uncovered %v, want %v", unseen, c.wantUnseen)
			}
		})
	}
}

func TestPersonIsCarriedOnThePick(t *testing.T) {
	picks, _ := PickSeats([]string{"security"}, seats, map[string]string{"reviewer-2": "priya"}, nil, 3)
	if len(picks) != 1 || picks[0].Person != "priya" {
		t.Fatalf("picks = %+v, want priya on reviewer-2", picks)
	}
}
