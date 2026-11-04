package change

import (
	"errors"
	"fmt"
	"slices"
)

// ActorKind says what sort of party caused an event.
type ActorKind string

// The actor kinds. An agent is an LLM role or the author's agent; system is the engine, the rules, timers and the
// reconciler (D56); github, jira and switch are the adapters that report what those systems said.
const (
	ActorHuman  ActorKind = "human"
	ActorAgent  ActorKind = "agent"
	ActorSystem ActorKind = "system"
	ActorGitHub ActorKind = "github"
	ActorJira   ActorKind = "jira"
	ActorSwitch ActorKind = "switch"
)

// AllActorKinds returns the actor kinds in a fresh slice.
func AllActorKinds() []ActorKind {
	return []ActorKind{ActorHuman, ActorAgent, ActorSystem, ActorGitHub, ActorJira, ActorSwitch}
}

// Valid reports whether k is one of the actor kinds.
func (k ActorKind) Valid() bool { return slices.Contains(AllActorKinds(), k) }

// Actor is who caused an event: a kind and an id within that kind.
type Actor struct {
	Kind ActorKind `json:"kind"`
	ID   ActorID   `json:"id"`
}

// Validate returns every problem with the actor, each naming its field.
func (a Actor) Validate() error {
	var errs []error
	if !a.Kind.Valid() {
		errs = append(errs, fmt.Errorf("actor.kind %q is not one of %v", a.Kind, AllActorKinds()))
	}
	if _, err := ParseActorID(string(a.ID)); err != nil {
		errs = append(errs, fmt.Errorf("actor.id: %w", err))
	}
	return errors.Join(errs...)
}
