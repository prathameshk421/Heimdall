package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/cardsfs"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/dsse"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/redact"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/secretscan"
)

type cardFrontMatter struct {
	AppliesTo       []string `yaml:"applies_to,omitempty"`
	Symbols         []string `yaml:"symbols,omitempty"`
	Triggers        []string `yaml:"triggers,omitempty"`
	Check           string   `yaml:"check,omitempty"`
	Proof           string   `yaml:"proof,omitempty"`
	Kind            string   `yaml:"kind,omitempty"`
	Source          string   `yaml:"source,omitempty"`
	Status          string   `yaml:"status,omitempty"`
	Scope           string   `yaml:"scope,omitempty"`
	Lens            string   `yaml:"lens,omitempty"`
	ApprovedBy      string   `yaml:"approved_by,omitempty"`
	ApprovedAt      string   `yaml:"approved_at,omitempty"`
	RejectedBy      string   `yaml:"rejected_by,omitempty"`
	RejectedAt      string   `yaml:"rejected_at,omitempty"`
	RejectionReason string   `yaml:"rejection_reason,omitempty"`
}

func formatCardMarkdown(c rulecard.Card) (string, error) {
	fm := cardFrontMatter{
		AppliesTo: c.AppliesTo, Symbols: c.Symbols, Triggers: c.Triggers,
		Check: c.Check, Proof: c.Proof, Kind: c.Kind, Source: c.Source,
		Status: string(c.Status), Scope: string(c.Scope), Lens: c.Lens,
		ApprovedBy: c.ApprovedBy, RejectedBy: c.RejectedBy, RejectionReason: c.RejectionReason,
	}
	if !c.ApprovedAt.IsZero() {
		fm.ApprovedAt = c.ApprovedAt.UTC().Format(time.RFC3339)
	}
	if !c.RejectedAt.IsZero() {
		fm.RejectedAt = c.RejectedAt.UTC().Format(time.RFC3339)
	}
	raw, err := yaml.Marshal(fm)
	if err != nil {
		return "", fmt.Errorf("encode card front matter: %w", err)
	}
	return "---\n" + string(raw) + "---\n" + strings.TrimSpace(c.Body) + "\n", nil
}

// writeInDir writes a plain file name inside dir using os.OpenRoot so traversal and symlink escapes are refused.
func writeInDir(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create folder: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open folder: %w", err)
	}
	defer func() { _ = root.Close() }()
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", name, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	return nil
}

// readUserFile reads a user-chosen file path via os.OpenRoot on its parent directory.
func readUserFile(path string) ([]byte, error) {
	clean := filepath.Clean(path)
	return readInDir(filepath.Dir(clean), filepath.Base(clean))
}

// runCardsImport parses pasted or existing rule files into scrubbed StatusCandidate cards locally.
func runCardsImport(args []string, env cardsEnv) int {
	fs := flag.NewFlagSet("heimdall cards import", flag.ContinueOnError)
	fs.SetOutput(env.errw)
	from := fs.String("from", "", "rule file path (AGENTS.md, CLAUDE.md, .cursorrules) or - for stdin")
	source := fs.String("source", "", "source label recorded on imported cards")
	dir := fs.String("dir", filepath.Join(defaultCardsWork, "files"), "folder to write candidate cards into")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *from == "" {
		writeLine(env.out, "usage: heimdall cards import --from FILE|- [--source label] [--dir dir]")
		return 2
	}
	raw, label, err := readImportSource(*from, *source, env.stdin)
	if err != nil {
		writeLine(env.out, "heimdall cards import: "+err.Error())
		return 1
	}
	scrubbed, rep := redact.Scrub(string(raw))
	cards, err := rulecard.ImportMarkdown(label, scrubbed)
	if err != nil {
		writeLine(env.out, "heimdall cards import: "+err.Error())
		return 1
	}
	if err := writeCandidateCards(*dir, cards, env.out); err != nil {
		writeLine(env.out, "heimdall cards import: "+err.Error())
		return 1
	}
	writeLine(env.out, fmt.Sprintf("imported %d candidate cards (%d local redactions). Review and run: heimdall cards approve NAME", len(cards), rep.TotalReplacements))
	return 0
}

func readImportSource(from, source string, stdin io.Reader) ([]byte, string, error) {
	if from == "-" {
		data, err := io.ReadAll(io.LimitReader(stdin, cardsfs.MaxCardBytes*8))
		if err != nil {
			return nil, "", fmt.Errorf("read stdin: %w", err)
		}
		if source == "" {
			source = "stdin"
		}
		return data, source, nil
	}
	data, err := readUserFile(from)
	if err != nil {
		return nil, "", err
	}
	if source == "" {
		source = filepath.Base(from)
	}
	return data, source, nil
}

func writeCandidateCards(dir string, cards []rulecard.Card, out io.Writer) error {
	for _, c := range cards {
		text, err := formatCardMarkdown(c)
		if err != nil {
			return err
		}
		if _, found := secretscan.RedactReport(text); len(found) > 0 {
			return fmt.Errorf("card %s still contains sensitive patterns after scrub; remove them first", c.Name)
		}
		if err := writeInDir(dir, c.Name, []byte(text)); err != nil {
			return err
		}
		writeLine(out, "candidate "+c.Name)
	}
	return nil
}

// runCardsApprove transitions one local candidate card to approved or rejected.
func runCardsApprove(args []string, env cardsEnv) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		writeLine(env.out, "usage: heimdall cards approve NAME [--dir dir] [--approver person] [--reject --reason text]")
		return 2
	}
	name := args[0]
	fs := flag.NewFlagSet("heimdall cards approve", flag.ContinueOnError)
	fs.SetOutput(env.errw)
	dir := fs.String("dir", filepath.Join(defaultCardsWork, "files"), "folder with the card files")
	approver := fs.String("approver", "", "person approving or rejecting (default: git user.name)")
	reject := fs.Bool("reject", false, "reject the candidate card instead of approving")
	reason := fs.String("reason", "", "rejection reason (required with --reject)")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		writeLine(env.out, "usage: heimdall cards approve NAME [--dir dir] [--approver person] [--reject --reason text]")
		return 2
	}
	who := resolveApprover(*approver, env)
	if err := approveLocalCard(*dir, name, who, *reject, *reason, env.now().UTC()); err != nil {
		writeLine(env.out, "heimdall cards approve: "+err.Error())
		return 1
	}
	action := "approved"
	if *reject {
		action = "rejected"
	}
	writeLine(env.out, fmt.Sprintf("%s %s by %s", action, name, who))
	return 0
}

func resolveApprover(flagVal string, env cardsEnv) string {
	if strings.TrimSpace(flagVal) != "" {
		return strings.TrimSpace(flagVal)
	}
	if name, _ := gitIdentity(context.Background(), env.git, "."); strings.TrimSpace(name) != "" {
		return strings.ToLower(strings.Join(strings.Fields(name), "-"))
	}
	return "owner"
}

func approveLocalCard(dir, name, who string, reject bool, reason string, at time.Time) error {
	raw, err := readInDir(dir, name)
	if err != nil {
		return err
	}
	card, err := cardsfs.Parse(name, string(raw))
	if err != nil {
		return err
	}
	var updated rulecard.Card
	if reject {
		updated, err = rulecard.Reject(card, who, reason, at)
	} else {
		updated, err = rulecard.Approve(card, who, at)
	}
	if err != nil {
		return err
	}
	text, err := formatCardMarkdown(updated)
	if err != nil {
		return err
	}
	if rep := checkCardText(name, text); len(rep.problems) > 0 {
		return errors.New(strings.Join(rep.problems, "; "))
	}
	return writeInDir(dir, name, []byte(text))
}

// runCardsSign signs one approved/active card into <name>.dsse.json using Ed25519 DSSE v1.
func runCardsSign(args []string, env cardsEnv) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		writeLine(env.out, "usage: heimdall cards sign NAME --key-file F [--key-id ID] [--init-key] [--dir dir]")
		return 2
	}
	name := args[0]
	fs := flag.NewFlagSet("heimdall cards sign", flag.ContinueOnError)
	fs.SetOutput(env.errw)
	dir := fs.String("dir", filepath.Join(defaultCardsWork, "files"), "folder with the card files")
	keyFile := fs.String("key-file", "", "path to base64 Ed25519 private key file")
	keyID := fs.String("key-id", "owner-key", "DSSE key identifier")
	initKey := fs.Bool("init-key", false, "generate a new Ed25519 key pair if --key-file does not exist")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *keyFile == "" {
		writeLine(env.out, "usage: heimdall cards sign NAME --key-file F [--key-id ID] [--init-key] [--dir dir]")
		return 2
	}
	if err := signCardFile(*dir, name, *keyFile, *keyID, *initKey); err != nil {
		writeLine(env.out, "heimdall cards sign: "+err.Error())
		return 1
	}
	writeLine(env.out, fmt.Sprintf("signed %s -> %s.dsse.json (key_id=%s)", name, name, *keyID))
	return 0
}

func signCardFile(dir, name, keyFile, keyID string, initKey bool) error {
	priv, err := loadOrInitEd25519Key(keyFile, initKey)
	if err != nil {
		return err
	}
	raw, err := readInDir(dir, name)
	if err != nil {
		return err
	}
	card, err := cardsfs.Parse(name, string(raw))
	if err != nil {
		return err
	}
	if card.Status != rulecard.StatusApproved && card.Status != rulecard.StatusActive {
		return fmt.Errorf("card %s has status %q; approve it before signing", name, card.Status)
	}
	env, err := dsse.Sign(dsse.PayloadTypeRuleCard, raw, keyID, priv)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("encode DSSE envelope: %w", err)
	}
	return writeInDir(dir, name+".dsse.json", append(encoded, '\n'))
}

func loadOrInitEd25519Key(path string, initKey bool) (ed25519.PrivateKey, error) {
	clean := filepath.Clean(path)
	keyDir, keyBase := filepath.Dir(clean), filepath.Base(clean)
	raw, err := readInDir(keyDir, keyBase)
	if errors.Is(err, os.ErrNotExist) && initKey {
		pub, priv, genErr := ed25519.GenerateKey(rand.Reader)
		if genErr != nil {
			return nil, fmt.Errorf("generate Ed25519 key: %w", genErr)
		}
		if wErr := writeInDir(keyDir, keyBase, []byte(base64.StdEncoding.EncodeToString(priv)+"\n")); wErr != nil {
			return nil, fmt.Errorf("write private key: %w", wErr)
		}
		if wErr := writeInDir(keyDir, keyBase+".pub", []byte(base64.StdEncoding.EncodeToString(pub)+"\n")); wErr != nil {
			return nil, fmt.Errorf("write public key: %w", wErr)
		}
		return priv, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(decoded) != ed25519.PrivateKeySize {
		return nil, errors.New("private key file must contain a base64-encoded 64-byte Ed25519 key")
	}
	return ed25519.PrivateKey(decoded), nil
}

// runCardsVerify checks <name>.dsse.json signature and byte equality against <name>.
func runCardsVerify(args []string, env cardsEnv) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		writeLine(env.out, "usage: heimdall cards verify NAME --pub-file F --key-id ID [--dir dir]")
		return 2
	}
	name := args[0]
	fs := flag.NewFlagSet("heimdall cards verify", flag.ContinueOnError)
	fs.SetOutput(env.errw)
	dir := fs.String("dir", filepath.Join(defaultCardsWork, "files"), "folder with the card files")
	pubFile := fs.String("pub-file", "", "path to base64 Ed25519 public key file")
	keyID := fs.String("key-id", "owner-key", "trusted DSSE key identifier")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *pubFile == "" {
		writeLine(env.out, "usage: heimdall cards verify NAME --pub-file F --key-id ID [--dir dir]")
		return 2
	}
	verifiedKey, err := verifyCardFile(*dir, name, *pubFile, *keyID)
	if err != nil {
		writeLine(env.out, "heimdall cards verify: "+err.Error())
		return 1
	}
	writeLine(env.out, fmt.Sprintf("verified %s (key_id=%s)", name, verifiedKey))
	return 0
}

func verifyCardFile(dir, name, pubFile, keyID string) (string, error) {
	pubRaw, err := readUserFile(pubFile)
	if err != nil {
		return "", fmt.Errorf("read public key: %w", err)
	}
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(pubRaw)))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return "", errors.New("public key file must contain a base64-encoded 32-byte Ed25519 public key")
	}
	cardRaw, err := readInDir(dir, name)
	if err != nil {
		return "", err
	}
	envRaw, err := readInDir(dir, name+".dsse.json")
	if err != nil {
		return "", fmt.Errorf("read DSSE sidecar: %w", err)
	}
	var env dsse.Envelope
	if err := json.Unmarshal(envRaw, &env); err != nil {
		return "", fmt.Errorf("decode DSSE sidecar: %w", err)
	}
	payload, verifiedKey, err := dsse.Verify(env, dsse.PayloadTypeRuleCard, map[string]ed25519.PublicKey{keyID: ed25519.PublicKey(pub)})
	if err != nil {
		return "", err
	}
	if !bytes.Equal(payload, cardRaw) {
		return "", errors.New("signed payload bytes do not match card file on disk")
	}
	return verifiedKey, nil
}

// runCardsExport exports approved/active cards into AGENTS.md, CLAUDE.md, or .cursor/rules/heimdall.mdc.
func runCardsExport(args []string, env cardsEnv) int {
	fs := flag.NewFlagSet("heimdall cards export", flag.ContinueOnError)
	fs.SetOutput(env.errw)
	target := fs.String("target", "agents", "export target: agents, claude, or cursor")
	dir := fs.String("dir", filepath.Join(defaultCardsWork, "files"), "folder with the card files")
	outPath := fs.String("out", "", "output file path (default depends on --target)")
	toStdout := fs.Bool("stdout", false, "write exported content to stdout instead of a file")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		writeLine(env.out, "usage: heimdall cards export --target agents|claude|cursor [--dir dir] [--out file] [--stdout]")
		return 2
	}
	dest, err := resolveExportDest(rulecard.ExportTarget(*target), *outPath)
	if err != nil {
		writeLine(env.out, "heimdall cards export: "+err.Error())
		return 2
	}
	cards, err := loadLocalCards(*dir)
	if err != nil {
		writeLine(env.out, "heimdall cards export: "+err.Error())
		return 1
	}
	existing := ""
	if !*toStdout {
		if raw, err := readUserFile(dest); err == nil {
			existing = string(raw)
		}
	}
	rendered, err := rulecard.ExportMarkdown(rulecard.ExportTarget(*target), cards, existing)
	if err != nil {
		writeLine(env.out, "heimdall cards export: "+err.Error())
		return 1
	}
	if *toStdout {
		_, _ = io.WriteString(env.out, rendered)
		return 0
	}
	cleanDest := filepath.Clean(dest)
	if err := writeInDir(filepath.Dir(cleanDest), filepath.Base(cleanDest), []byte(rendered)); err != nil {
		writeLine(env.out, "heimdall cards export: "+err.Error())
		return 1
	}
	writeLine(env.out, "exported to "+dest)
	return 0
}

func resolveExportDest(target rulecard.ExportTarget, custom string) (string, error) {
	def, err := rulecard.DefaultExportPath(target)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(custom) != "" {
		return custom, nil
	}
	return def, nil
}

func loadLocalCards(dir string) ([]rulecard.Card, error) {
	names, err := cardFileNames(dir)
	if err != nil {
		return nil, err
	}
	cards := make([]rulecard.Card, 0, len(names))
	for _, name := range names {
		raw, err := readInDir(dir, name)
		if err != nil {
			continue
		}
		card, err := cardsfs.Parse(name, string(raw))
		if err != nil {
			continue
		}
		cards = append(cards, card)
	}
	return cards, nil
}

// runCardsOutcomes displays owner-only calibration outcomes for the authenticated owner's cards.
func runCardsOutcomes(args []string, env cardsEnv) int {
	client, o, ok := remoteSetup("outcomes", args, env)
	if !ok {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), cardsRemoteTimeout)
	defer cancel()
	outcomes, err := client.outcomes(ctx)
	if err != nil {
		writeLine(env.out, "heimdall cards outcomes: "+err.Error())
		return 1
	}
	if o.jsonOut {
		enc := json.NewEncoder(env.out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{"outcomes": outcomes})
		return 0
	}
	if len(outcomes) == 0 {
		writeLine(env.out, "no active cards found for your account")
		return 0
	}
	writeLine(env.out, "CARD\tSTATUS\tWEIGHT\tKEPT\tDISMISSED\tOVERRIDDEN\tIGNORED\tPROVEN\tHINT")
	for _, item := range outcomes {
		writeLine(env.out, fmt.Sprintf("%s\t%s\t%.2f\t%d\t%d\t%d\t%d\t%d\t%s",
			item.CardName, item.Status, item.Weight, item.Kept, item.Dismissed, item.Overridden, item.Ignored, item.Proven, item.Hint))
	}
	return 0
}
