package main

import (
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// reprofileEvents records a policy owner's request to revise the repository's promoted sandbox profile (D184). The
// request is one event on the change where it was posted: the profile row reads it as a pending revision, and the
// profiler gets the note with what is promoted now.
func (r *commandRuntime) reprofileEvents(cid id.ChangeID, state []change.Event, person string, source commands.Source, sourceRef string, actor change.Actor, cmd commands.Reprofile) ([]change.Event, error) {
	repo, _, err := repoOf(state, cid)
	if err != nil {
		return nil, fmt.Errorf("read the repository of %s: %w", cid, err)
	}
	if repo == "" {
		return nil, errors.New("the change names no repository")
	}
	request := change.ProfileRequested{Repo: repo, By: change.ActorID(person), Note: cmd.Note}
	if err := request.Validate(); err != nil {
		return nil, fmt.Errorf("validate profile request: %w", err)
	}
	ev, err := r.event(cid, actor, source, change.EventProfileRequested, request, sourceRef)
	if err != nil {
		return nil, err
	}
	return []change.Event{ev}, nil
}
