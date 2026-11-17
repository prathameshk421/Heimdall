package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/announce"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/collaboration"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/intake"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reconcile"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/roomstream"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

// assembleNarrator builds the milestone narrator: one plain line per step of every change, in the change's room and in
// the configured feed rooms (the Telegram group, the Mattermost and Discord test and review channels).
func assembleNarrator(d deps, ad adapters, store *postgres.Store) (*announce.Narrator, error) {
	feeds := make([]announce.Feed, 0, len(d.configuration.Feeds))
	for _, f := range d.configuration.Feeds {
		feeds = append(feeds, announce.Feed{Room: change.RoomID(f.RoomID), Audience: announce.Audience(f.Audience)})
	}
	n, err := announce.New(announce.Config{History: store, Events: store, Cursors: store, Chat: ad.chat, Feeds: feeds, ReviewRoom: ad.reviewsRoom, TestsRoom: ad.testsRoom, Tickets: ad.tickets, Comments: ad.vcs, Inline: inlineOf(ad.vcs),
		Warn: func(ctx context.Context, err error) {
			slog.Default().ErrorContext(ctx, "pull request comment failed; the next event retries", "error", err)
		}})
	if err != nil {
		return nil, fmt.Errorf("build narrator: %w", err)
	}
	return n, nil
}

func assembleCollaboration(store *postgres.Store, ad adapters, eng reconcile.Recorder, gen ports.IDs, clk ports.Clock, refreshEvery time.Duration, intakeCase *intake.Intake, profiles mcp.Profiles) (roomStreamRunner, commentPoller, *commandRuntime, error) {
	runtime, err := newCommandRuntime(store, ad, eng, gen, clk, prIntake{in: intakeCase}, profiles)
	if err != nil {
		return nil, nil, nil, err
	}
	stream, ok := ad.chat.(ports.ChatStream)
	if !ok {
		return nil, nil, nil, errors.New("selected chat adapter does not implement the Switch room stream")
	}
	scan, err := newPostScanner(runtime, slog.Default())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build post scanner: %w", err)
	}
	roomConsumer, err := roomstream.New(roomstream.Config{Rooms: store, Cursors: store, Stream: stream, Handler: runtime.processor, Standing: ad.standingRooms, RefreshEvery: refreshEvery, Wait: clock.Wait, Watch: scan.Watch,
		Probe: func(ctx context.Context, room change.RoomID) error {
			_, _, err := ad.chat.Read(ctx, room, "")
			return err
		}})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build Switch room consumer: %w", err)
	}
	commentConsumer, err := collaboration.NewGitHubCommentPoller(collaboration.CommentPollConfig{Repos: ad.repos, PRs: ad.vcs, Comments: ad.vcs, Cursors: store, Handler: runtime, Clock: clk})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build GitHub comment consumer: %w", err)
	}
	return roomConsumer, commentConsumer, runtime, nil
}

// inlineOf returns the line comment writer of the code host, when it has one.
func inlineOf(vcs any) announce.InlineComments {
	if w, ok := vcs.(announce.InlineComments); ok {
		return w
	}
	return nil
}
