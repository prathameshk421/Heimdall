package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/cardsfs"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/knowledge"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

// cardReader serves a reviewer's personal context cards. The person comes from the base-commit policy's agent
// mapping, never from the tool arguments, so a reviewer can read only the cards of the person it speaks for.
type cardReader struct {
	store  *cardsfs.Store
	scopes knowledge.PolicyScopes
}

// newReviewerReaders builds the two private-context readers a reviewer has: the encrypted pack and the plain cards.
func newReviewerReaders(d deps, blobs ports.BlobStore, pool *pgxpool.Pool, clk ports.Clock, code knowledge.PolicyCode) (mcp.PackReader, mcp.Cards, error) {
	packs, err := newPackReader(d, blobs, pool, clk, code)
	if err != nil {
		return nil, nil, err
	}
	cards, err := newCardReader(d.configuration.CardsDir, knowledge.PolicyScopes{Code: code, Decode: policyyaml.Decode})
	if err != nil {
		return nil, nil, err
	}
	return packs, cards, nil
}

// newCardReader returns nil when no cards directory is configured, which leaves the read_card tool off.
func newCardReader(dir string, scopes knowledge.PolicyScopes) (mcp.Cards, error) {
	if dir == "" {
		return nil, nil
	}
	store, err := cardsfs.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("open personal cards: %w", err)
	}
	return cardReader{store: store, scopes: scopes}, nil
}

func (r cardReader) Read(ctx context.Context, p mcp.Principal, a mcp.Assignment, file string) (string, error) {
	if p.Agent == "" || p.Role != workflow.RoleReviewer || a.Task.Role != workflow.RoleReviewer {
		return "", errors.New("personal cards are available only to an authenticated reviewer")
	}
	person, _, err := r.scopes.PersonForAgent(ctx, p.Agent, change.Repo(a.Task.Change.Repo), a.Task.Change.BaseSHA)
	if err != nil {
		return "", fmt.Errorf("resolve reviewer person from base policy: %w", err)
	}
	if strings.TrimSpace(file) == "" {
		names, err := r.store.List(person)
		if err != nil {
			return "", fmt.Errorf("list cards: %w", err)
		}
		if len(names) == 0 {
			return "You have no personal cards yet.", nil
		}
		return "Your cards (call read_card with one of these file names):\n- " + strings.Join(names, "\n- "), nil
	}
	content, err := r.store.Read(person, file)
	if errors.Is(err, cardsfs.ErrNotFound) {
		return "", mcp.ErrCardNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read card: %w", err)
	}
	return content, nil
}

// cardBudget caps the card text put into one task, so the cards never crowd out the diff.
const cardBudget = 12 * 1024

// ForTask returns the reviewer's cards that apply to the files the change touches, most specific first.
func (r cardReader) ForTask(ctx context.Context, p mcp.Principal, a mcp.Assignment) (string, error) {
	if p.Agent == "" || p.Role != workflow.RoleReviewer || a.Task.Role != workflow.RoleReviewer {
		return "", errors.New("personal cards are available only to an authenticated reviewer")
	}
	person, _, err := r.scopes.PersonForAgent(ctx, p.Agent, change.Repo(a.Task.Change.Repo), a.Task.Change.BaseSHA)
	if err != nil {
		return "", fmt.Errorf("resolve reviewer person from base policy: %w", err)
	}
	cards, err := r.store.Relevant(person, a.Task.Inputs.FilesChanged, cardBudget)
	if err != nil {
		return "", fmt.Errorf("select cards: %w", err)
	}
	var b strings.Builder
	for _, card := range cards {
		fmt.Fprintf(&b, "## %s", card.Name)
		if card.Kind != "" {
			fmt.Fprintf(&b, " (%s)", card.Kind)
		}
		if card.Source != "" {
			fmt.Fprintf(&b, " [from: %s]", card.Source)
		}
		b.WriteString("\n" + card.Body + "\n\n")
	}
	return strings.TrimSpace(b.String()), nil
}
