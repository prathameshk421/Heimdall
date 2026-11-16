package testset

import (
	"reflect"
	"testing"
)

func TestValidAnswer(t *testing.T) {
	cases := []struct {
		name string
		a    Answer
		ok   bool
	}{
		{"weak with a killing test", Answer{Kind: AnswerWeak, Killed: true}, true},
		{"weak without a kill", Answer{Kind: AnswerWeak}, false},
		{"equivalent with a sentence", Answer{Kind: AnswerEquivalent, Sentence: "the branch is unreachable"}, true},
		{"equivalent, blank sentence", Answer{Kind: AnswerEquivalent, Sentence: "   "}, false},
		{"unspecified with a sentence", Answer{Kind: AnswerUnspecified, Sentence: "does the spec allow zero?"}, true},
		{"unspecified, no sentence", Answer{Kind: AnswerUnspecified}, false},
		{"unknown kind", Answer{Kind: "skip"}, false},
	}
	for _, c := range cases {
		if err := ValidAnswer(c.a); (err == nil) != c.ok {
			t.Errorf("%s: ValidAnswer = %v, want ok=%v", c.name, err, c.ok)
		}
	}
}

// TestOpenSurvivorsResetsOnANewHead: a valid answer closes a survivor only for the head it was given on.
func TestOpenSurvivorsResetsOnANewHead(t *testing.T) {
	survivors := []string{"m1", "m2", "m3"}
	answers := []Answer{
		{Survivor: "m1", Head: "h1", Kind: AnswerWeak, Killed: true},
		{Survivor: "m2", Head: "h1", Kind: AnswerEquivalent, Sentence: "same result"},
		{Survivor: "m3", Head: "h1", Kind: AnswerWeak}, // not killed: does not count
	}
	if got, want := OpenSurvivors("h1", survivors, answers), []string{"m3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("on h1: open = %v, want %v", got, want)
	}
	if got, want := OpenSurvivors("h2", survivors, answers), survivors; !reflect.DeepEqual(got, want) {
		t.Fatalf("on the new head h2: open = %v, want all of %v", got, want)
	}
}

func TestOpenSurvivorsDeduplicatesAndKeepsOrder(t *testing.T) {
	got := OpenSurvivors("h", []string{"b", "a", "b"}, nil)
	if want := []string{"b", "a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("open = %v, want %v", got, want)
	}
}
