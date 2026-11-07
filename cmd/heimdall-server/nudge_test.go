package main

import (
	"testing"
	"time"
)

func TestAStalledTaskIsNudgedAfterASilentWindowUpToTheCap(t *testing.T) {
	last := time.Date(2026, 11, 7, 11, 18, 0, 0, time.UTC)
	cases := []struct {
		name  string
		after time.Duration
		sent  int
		want  bool
	}{
		{"recent activity", stallAfter - time.Second, 0, false},
		{"first window passed", stallAfter, 0, true},
		{"long silence", 4 * stallAfter, 1, true},
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

func TestAckNudgesBackOffByDoublingTheGap(t *testing.T) {
	at := time.Date(2026, 11, 7, 12, 58, 0, 0, time.UTC)
	cases := []struct {
		after time.Duration
		sent  int
		want  bool
	}{
		{44 * time.Second, 0, false},
		{45 * time.Second, 0, true},
		{90 * time.Second, 1, false},
		{135 * time.Second, 1, true},
		{300 * time.Second, 2, false},
		{315 * time.Second, 2, true},
		{time.Hour, nudgeMax, false},
	}
	for _, c := range cases {
		if got := ackNudgeDue(at.Add(c.after), at, c.sent); got != c.want {
			t.Errorf("after %s with %d sent: due = %v, want %v", c.after, c.sent, got, c.want)
		}
	}
}
