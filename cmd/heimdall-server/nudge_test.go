package main

import (
	"testing"
	"time"
)

func TestAStalledTaskIsNudgedAfterEachSilentWindowUpToTheCap(t *testing.T) {
	last := time.Date(2026, 11, 7, 11, 18, 0, 0, time.UTC)
	cases := []struct {
		name  string
		after time.Duration
		sent  int
		want  bool
	}{
		{"recent activity", stallAfter - time.Second, 0, false},
		{"first window passed", stallAfter, 0, true},
		{"second nudge waits a second window", stallAfter + time.Minute, 1, false},
		{"second window passed", 2 * stallAfter, 1, true},
		{"cap reached", 100 * stallAfter, stallMax, false},
	}
	for _, c := range cases {
		if got := stallDue(last.Add(c.after), last, c.sent); got != c.want {
			t.Errorf("%s: stallDue = %v, want %v", c.name, got, c.want)
		}
	}
	if stallDue(last, time.Time{}, 0) {
		t.Error("a task with no known activity time must not be nudged")
	}
}
