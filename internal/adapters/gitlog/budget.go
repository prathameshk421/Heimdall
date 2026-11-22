package gitlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The monthly budget file of ACTION_MODE 2.3 and 7: budget/<yyyy-mm>.json on the state ref, one entry per run so that
// a repeated gate run cannot count one proof run twice.
const (
	// budgetDir is the directory of the monthly files on the state ref.
	budgetDir = "budget"
	// budgetVersion is the one version of the budget file this build writes and reads.
	budgetVersion = 1
	// maxBudgetBytes bounds one budget file. A month of 10,000 runs of 100 bytes stays far below it.
	maxBudgetBytes = 1 << 20
	// MaxRunKey bounds the key of one budget entry. A GitHub Actions run id is at most 20 characters.
	MaxRunKey = 200
	// MonthLayout is the format of the month in a file name, in UTC.
	MonthLayout = "2006-01"
	// maxMonthlyMicros bounds a month's total: $1,000,000, the bound of one cost record. A sum over it is not believed.
	maxMonthlyMicros = 1_000_000_000_000
)

// ErrBudgetFile marks a monthly budget file that cannot be believed: malformed JSON, another month, a negative amount
// or one over its limit. The caller reports the month as not known; it never reads as $0 spent.
var ErrBudgetFile = errors.New("monthly budget file is malformed")

// ErrBudgetRunRecorded is returned when a run key is already stored with another amount. A run key must always name the
// same cost, as an effect key always names the same effect.
var ErrBudgetRunRecorded = errors.New("budget run key already recorded with a different amount")

// errNothingToWrite says the entry is already stored with the same amount: the caller wrote it before.
var errNothingToWrite = errors.New("nothing to write")

var reMonth = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// monthlyBudget is the content of budget/<yyyy-mm>.json: the cost of every run of the month, in whole micro dollars,
// keyed by the run. The gate adds one entry per proof run, so a repeat of the same gate run is a no-op.
type monthlyBudget struct {
	Version int              `json:"version"`
	Month   string           `json:"month"`
	Runs    map[string]int64 `json:"runs"`
}

// budgetPath is the file of one month.
func budgetPath(yyyyMM string) string { return budgetDir + "/" + yyyyMM + ".json" }

// checkMonth refuses a month that is not yyyy-mm, so no other file name can be built from it.
func checkMonth(yyyyMM string) error {
	if !reMonth.MatchString(yyyyMM) {
		return fmt.Errorf("month %q is not yyyy-mm", yyyyMM)
	}
	return nil
}

// checkRunKey refuses a run key that is empty, too long, or carries a space, a control character or a non-ASCII byte.
// The key is a JSON map key, so it is never put in a path or a shell.
func checkRunKey(key string) error {
	switch {
	case key == "":
		return errors.New("the run key is empty")
	case len(key) > MaxRunKey:
		return fmt.Errorf("the run key is longer than %d bytes", MaxRunKey)
	}
	for _, r := range key {
		if r <= ' ' || r > '~' {
			return errors.New("the run key has a space, a control character or a non-ASCII character")
		}
	}
	return nil
}

// parseBudget decodes one budget file strictly and checks its version and month. A file with no runs is a month that
// spent nothing, which is known.
func parseBudget(yyyyMM string, data []byte) (monthlyBudget, error) {
	var b monthlyBudget
	if err := decodeStrict(data, &b); err != nil {
		return monthlyBudget{}, fmt.Errorf("%w: decode", ErrBudgetFile)
	}
	switch {
	case b.Version != budgetVersion:
		return monthlyBudget{}, fmt.Errorf("%w: version %d, want %d", ErrBudgetFile, b.Version, budgetVersion)
	case b.Month != yyyyMM:
		return monthlyBudget{}, fmt.Errorf("%w: it holds month %q, not %q", ErrBudgetFile, b.Month, yyyyMM)
	}
	return b, nil
}

// total is the sum of the month's runs. A negative amount, or one that overflows the bound, is a malformed file.
func (b monthlyBudget) total() (int64, error) {
	var sum int64
	for key, micros := range b.Runs {
		if micros < 0 {
			return 0, fmt.Errorf("%w: run %q has %d", ErrBudgetFile, key, micros)
		}
		if sum > maxMonthlyMicros-micros {
			return 0, fmt.Errorf("%w: the total is over %d", ErrBudgetFile, int64(maxMonthlyMicros))
		}
		sum += micros
	}
	return sum, nil
}

// MonthConfig holds what NewMonthReader needs. It has no key: a budget file carries no MAC, because a forged month
// total can only stop a run earlier or later, and the provider's own spend limit is the hard stop (ACTION_MODE 7).
type MonthConfig struct {
	// Repo is the repository that holds the ref.
	Repo change.Repo
	// Git reaches GitHub. *github.ActionAPI satisfies it.
	Git Git
	// Ref is the full ref name. Empty means DefaultRef.
	Ref string
}

// MonthReader reads budget/<yyyy-mm>.json from the state ref. The proof job reads it read-only before its first call,
// and it holds no MAC key.
type MonthReader struct {
	git  Git
	repo change.Repo
	ref  string
}

// NewMonthReader checks cfg and returns a MonthReader. It needs no key, so a step that may not write the ref can still
// read the month's spend.
func NewMonthReader(cfg MonthConfig) (*MonthReader, error) {
	if cfg.Git == nil {
		return nil, errors.New("gitlog: Git is required")
	}
	if _, err := change.ParseRepo(cfg.Repo.String()); err != nil {
		return nil, fmt.Errorf("gitlog: %w", err)
	}
	ref := cfg.Ref
	if ref == "" {
		ref = DefaultRef
	}
	if !reRefName.MatchString(ref) || containsDotDot(ref) {
		return nil, errors.New("gitlog: the ref must be a full name such as refs/heimdall/state")
	}
	return &MonthReader{git: cfg.Git, repo: cfg.Repo, ref: ref}, nil
}

// ReadMonthlySpend returns what the month spent, in whole micro dollars, and whether the total is known. A state ref
// that does not exist yet, or a month with no file yet, is (0, true, nil): nothing was spent, and that is a fact. Only a
// read, decode or integrity failure is unknown, with the error that names it.
func (m *MonthReader) ReadMonthlySpend(ctx context.Context, yyyyMM string) (int64, bool, error) {
	if err := checkMonth(yyyyMM); err != nil {
		return 0, false, fmt.Errorf("monthly spend: %w", err)
	}
	b, found, err := m.read(ctx, yyyyMM)
	if err != nil {
		return 0, false, fmt.Errorf("monthly spend of %s: %w", yyyyMM, err)
	}
	if !found {
		return 0, true, nil
	}
	total, err := b.total()
	if err != nil {
		return 0, false, fmt.Errorf("monthly spend of %s: %w", yyyyMM, err)
	}
	return total, true, nil
}

// read returns the month's file at the current tip. found is false when the ref or the file does not exist yet.
func (m *MonthReader) read(ctx context.Context, yyyyMM string) (monthlyBudget, bool, error) {
	tip, err := readTip(ctx, m.git, m.repo, m.ref)
	if err != nil || tip == "" {
		return monthlyBudget{}, false, err
	}
	commit, err := m.git.GetCommit(ctx, m.repo, tip)
	if err != nil {
		return monthlyBudget{}, false, err
	}
	t, err := listTree(ctx, m.git, m.repo, commit.Tree)
	if err != nil {
		return monthlyBudget{}, false, err
	}
	sha, err := budgetFileSHA(t, yyyyMM)
	if err != nil {
		return monthlyBudget{}, false, err
	}
	if sha == "" {
		return monthlyBudget{}, false, nil
	}
	data, err := readBlob(ctx, m.git, m.repo, sha, "budget file", maxBudgetBytes)
	if err != nil {
		return monthlyBudget{}, false, err
	}
	b, err := parseBudget(yyyyMM, data)
	if err != nil {
		return monthlyBudget{}, false, err
	}
	return b, true, nil
}

// budgetFileSHA returns the blob of the month's file, or "" when it is absent. A budget entry that is not a directory,
// or a file that is not a plain file, is a malformed store, never a month with no spend.
func budgetFileSHA(t tree, yyyyMM string) (id.SHA, error) {
	if d, ok := t[budgetDir]; ok && d.Type != "tree" {
		return "", fmt.Errorf("%w: %s is not a directory", ErrBudgetFile, budgetDir)
	}
	e, ok := t[budgetPath(yyyyMM)]
	if !ok {
		return "", nil
	}
	if e.Type != "blob" || e.Mode != "100644" {
		return "", fmt.Errorf("%w: %s is not a plain file", ErrBudgetFile, budgetPath(yyyyMM))
	}
	return e.SHA, nil
}

// ReadMonthlySpend is MonthReader.ReadMonthlySpend on the store's ref and GitHub client.
func (s *Store) ReadMonthlySpend(ctx context.Context, yyyyMM string) (int64, bool, error) {
	return (&MonthReader{git: s.git, repo: s.repo, ref: s.ref}).ReadMonthlySpend(ctx, yyyyMM)
}

// AddMonthlySpend adds deltaMicros to the month's file under runKey and moves the ref by compare-and-swap. A run key
// already stored with the same amount is a no-op, so a repeated gate run never counts one proof run twice; the same key
// with another amount is ErrBudgetRunRecorded. A zero amount writes nothing (ACTION_MODE 2.4, 2.5, 7).
func (s *Store) AddMonthlySpend(ctx context.Context, yyyyMM string, deltaMicros int64, runKey string) error {
	if err := checkMonth(yyyyMM); err != nil {
		return fmt.Errorf("add monthly spend: %w", err)
	}
	if err := checkRunKey(runKey); err != nil {
		return fmt.Errorf("add monthly spend: %w", err)
	}
	if deltaMicros < 0 {
		return fmt.Errorf("add monthly spend: %d micro dollars is negative", deltaMicros)
	}
	if deltaMicros == 0 {
		return nil
	}
	for range maxRebuilds {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("add monthly spend of %s: %w", yyyyMM, err)
		}
		snap, plan, err := s.planBudget(ctx, yyyyMM, deltaMicros, runKey)
		if errors.Is(err, errNothingToWrite) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("add monthly spend of %s: %w", yyyyMM, err)
		}
		err = s.publish(ctx, snap, plan, "heimdall state: budget "+yyyyMM)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errRefMoved) {
			return fmt.Errorf("add monthly spend of %s: %w", yyyyMM, err)
		}
	}
	return fmt.Errorf("add monthly spend of %s: the ref moved %d times in a row: %w", yyyyMM, maxRebuilds, ports.ErrUnavailable)
}

// planBudget reads the month at the current tip and returns the snapshot and the commit that adds one entry. It refuses
// a file it cannot believe, and reports errNothingToWrite when the entry is already stored with the same amount.
func (s *Store) planBudget(ctx context.Context, yyyyMM string, deltaMicros int64, runKey string) (*snapshot, *writePlan, error) {
	snap := &snapshot{}
	b, err := s.readMonthAt(ctx, yyyyMM, snap)
	if err != nil {
		return nil, nil, err
	}
	if old, ok := b.Runs[runKey]; ok {
		if old == deltaMicros {
			return nil, nil, errNothingToWrite
		}
		return nil, nil, fmt.Errorf("%w: run %q is %d, not %d", ErrBudgetRunRecorded, runKey, old, deltaMicros)
	}
	if b.Runs == nil {
		b.Runs = map[string]int64{}
	}
	b.Runs[runKey] = deltaMicros
	if _, err := b.total(); err != nil {
		return nil, nil, err
	}
	data, err := json.Marshal(b)
	if err != nil {
		return nil, nil, fmt.Errorf("encode budget: %w", err)
	}
	data = append(data, '\n')
	if int64(len(data)) > maxBudgetBytes {
		return nil, nil, &LimitError{What: "budget file", Max: maxBudgetBytes, Got: int64(len(data))}
	}
	return snap, &writePlan{files: []file{{path: budgetPath(yyyyMM), content: data}}}, nil
}

// readMonthAt fills snap with the tip it read and returns the month as that tip holds it. A ref that does not exist
// yet, or a month with no file, gives an empty month. A file it cannot believe is ErrBudgetFile.
func (s *Store) readMonthAt(ctx context.Context, yyyyMM string, snap *snapshot) (monthlyBudget, error) {
	b := monthlyBudget{Version: budgetVersion, Month: yyyyMM, Runs: map[string]int64{}}
	tip, err := readTip(ctx, s.git, s.repo, s.ref)
	if err != nil {
		return b, err
	}
	if tip == "" {
		return b, nil
	}
	commit, err := s.git.GetCommit(ctx, s.repo, tip)
	if err != nil {
		return b, err
	}
	snap.tip, snap.rootTree = tip, commit.Tree
	t, err := s.listTree(ctx, snap.rootTree)
	if err != nil {
		return b, err
	}
	sha, err := budgetFileSHA(t, yyyyMM)
	if err != nil {
		return b, err
	}
	if sha == "" {
		return b, nil
	}
	data, err := s.blob(ctx, sha, "budget file", maxBudgetBytes)
	if err != nil {
		return b, err
	}
	return parseBudget(yyyyMM, data)
}
