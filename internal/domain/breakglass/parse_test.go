package breakglass

import (
	"strings"
	"testing"
	"time"
)

func TestParseBreakGlassCommands(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	base := Comment{ID: 7, Login: "Alice", UserType: "User", Body: "/heimdall break-glass production outage, rollback blocked", CreatedAt: now, UpdatedAt: now}
	record := &Recorded{Login: "Alice", Head: "head"}
	tests := []struct {
		name     string
		c        Comment
		recorded *Recorded
		kind     Kind
		drop     DropReason
	}{
		{"record", base, nil, Record, ""},
		{"edited", func() Comment { c := base; c.UpdatedAt = now.Add(time.Second); return c }(), nil, "", DropEdited},
		{"bot", func() Comment { c := base; c.UserType = "Bot"; return c }(), nil, "", DropBot},
		{"unauthorized", func() Comment { c := base; c.Login = "mallory"; return c }(), nil, "", DropUnauthorized},
		{"short reason", func() Comment { c := base; c.Body = "/heimdall break-glass too short"; return c }(), nil, "", DropShortReason},
		{"self review", func() Comment { c := base; c.Body = "/heimdall break-glass-reviewed"; return c }(), record, "", DropSelfReview},
		{"other review", func() Comment { c := base; c.Login = "bob"; c.Body = "/heimdall break-glass-reviewed"; return c }(), record, Review, ""},
		{"injection retained as text", func() Comment {
			c := base
			c.Body = "/heimdall break-glass " + strings.Repeat("<script>@everyone `", 2)
			return c
		}(), nil, Record, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allow := []string{"alice", "bob"}
			got, drop := Parse(tt.c, allow, "head", tt.recorded)
			if got.Kind != tt.kind || drop != tt.drop {
				t.Fatalf("Parse() = %+v, %q; want kind %q drop %q", got, drop, tt.kind, tt.drop)
			}
		})
	}
}
