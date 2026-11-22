package collaboration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// linkTTL is how long a link code stays open: long enough to switch to GitHub, short enough that a code seen by someone
// else is soon useless.
const linkTTL = 30 * time.Minute

// askToLink answers a chat sender Heimdall does not know with a one-time code and where to comment it (D194). The same
// open code is given again until it is used or expires, so asking twice does not pile up codes.
func (p *Processor) askToLink(ctx context.Context, in Input) error {
	room := in.Target.Room.RoomID
	if room == "" {
		return errors.New("link request has no chat room")
	}
	now := p.clock.Now()
	code, err := p.links.OpenLinkCode(ctx, change.SenderID(in.Command.Sender), room, now, now.Add(linkTTL))
	if err != nil {
		return fmt.Errorf("open link code: %w", err)
	}
	return p.replies.Reply(ctx, in.Target, commands.SourceChat, in.SourceRef, linkAsk(code.Code))
}

// linkAsk is plain text: chat shows Markdown as typed.
func linkAsk(code string) string {
	return "I don't know this chat account yet, so I can't act on it. To link it to you, comment this on any pull " +
		"request Heimdall is reviewing, from your own GitHub account:\n/heimdall link " + code +
		"\nThe code works for 30 minutes. You only do this once for each chat account."
}

// handleLink takes `/heimdall link <code>`. Only a GitHub comment redeems a code: GitHub has already proved the login,
// and the chat account is the thing being proven, so a code typed in chat proves nothing and is pointed to GitHub.
func (p *Processor) handleLink(ctx context.Context, in Input, code string) error {
	if in.Command.Source != commands.SourceGitHub {
		isAgent, err := p.identity.IsAgent(ctx, in.Target, in.Command.Source, in.Command.Sender)
		if err != nil {
			return fmt.Errorf("resolve link sender kind: %w", err)
		}
		if isAgent {
			return nil
		}
		return p.replies.Reply(ctx, in.Target, in.Command.Source, in.SourceRef,
			"Comment that on GitHub, not here: /heimdall link "+code+" on any pull request Heimdall is reviewing, from "+
				"your own GitHub account. That is how I know this chat account is yours.")
	}
	login := in.Command.Sender
	used, err := p.links.RedeemLinkCode(ctx, code, login, p.clock.Now())
	if errors.Is(err, ports.ErrNoLinkCode) {
		return p.replies.Reply(ctx, in.Target, commands.SourceGitHub, in.SourceRef,
			"That link code is unknown, already used or expired. Send `heimdall help` in chat to get a new one.")
	}
	if err != nil {
		return fmt.Errorf("redeem link code: %w", err)
	}
	people, err := p.identity.People(ctx, in.Target)
	if err != nil {
		return fmt.Errorf("load link people: %w", err)
	}
	name, mapped := people.Lookup(commands.SourceGitHub, login)
	onGitHub, inChat := linkResult(name, mapped && name != "", login)
	if err := p.replies.Reply(ctx, in.Target, commands.SourceGitHub, in.SourceRef, onGitHub); err != nil {
		return err
	}
	room := Target{Room: ports.RoomBinding{ChangeID: in.Target.ChangeID, RoomID: used.Room}, ChangeID: in.Target.ChangeID}
	return p.replies.Reply(ctx, room, commands.SourceChat, "link:"+used.Code, inChat)
}

// linkResult is what the GitHub comment and the chat room are told: who the chat account now is, or what is still
// missing when the login is not a person in this repository's policy.
func linkResult(name string, mapped bool, login string) (string, string) {
	if !mapped {
		missing := login + " is not in this repository's people list, so Heimdall still won't take commands from it. " +
			"A policy owner can add it to heimdall.yaml."
		return "Linked the chat account to @" + login + ", but " + missing,
			"Linked this chat account to GitHub " + login + ", but " + missing
	}
	return "Linked. That chat account now speaks for **" + name + "** (@" + login + "), with the same rights as here.",
		"Linked: this chat account is now " + name + " (GitHub " + login + "). Send heimdall help to see what you can do."
}
