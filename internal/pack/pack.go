package pack

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Section is one approved markdown pack section.
type Section struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// Manifest is signed and encrypted with the pack; hashes bind the owner's approval to its contents.
type Manifest struct {
	Person          string            `json:"person"`
	Version         int               `json:"version"`
	CreatedAt       time.Time         `json:"created_at"`
	TranscriptDays  int               `json:"transcript_days"`
	SourceSummary   []string          `json:"source_summary"`
	ScannerVersions map[string]string `json:"scanner_versions"`
	FileHashes      map[string]string `json:"file_hashes"`
	Canary          string            `json:"canary"`
	Signature       string            `json:"signature"`
}

// Distiller converts already-redacted source documents into concise markdown sections with provenance.
type Distiller interface {
	Distill(context.Context, []SourceDocument) ([]Section, error)
}

// Scanner is the two-pass secret/PII scanner. Production uses the embedded Gitleaks rule set.
type Scanner interface {
	Scan(string) (string, []Redaction, error)
}

// OwnerReview presents the complete pack and removed-material summary, then returns explicit approval.
type OwnerReview interface {
	Approve(context.Context, Manifest, []Section, []Redaction) (bool, error)
}

// Cipher encrypts plaintext for the configured age recipient.
type Cipher interface {
	Encrypt(context.Context, []byte) ([]byte, error)
	Decrypt(context.Context, []byte) ([]byte, error)
}

// Uploader stores only ciphertext and metadata.
type Uploader interface {
	Upload(context.Context, string, []byte, Manifest) error
}

// BuildInput contains local source material and the caller's signing identity.
type BuildInput struct {
	Person         string
	Version        int
	CreatedAt      time.Time
	TranscriptDays int
	Documents      []SourceDocument
	Canary         string
	PrivateKey     ed25519.PrivateKey
}

// Draft is the redacted material presented for owner review before signing or upload.
type Draft struct {
	Person         string      `json:"person"`
	Version        int         `json:"version"`
	CreatedAt      time.Time   `json:"created_at"`
	TranscriptDays int         `json:"transcript_days"`
	Canary         string      `json:"canary"`
	Sections       []Section   `json:"sections"`
	Removed        []Redaction `json:"removed"`
}

// BuildDeps are required collaborators. Nil dependencies fail closed.
type BuildDeps struct {
	Distiller Distiller
	Scanner   Scanner
	Review    OwnerReview
	Cipher    Cipher
	Upload    Uploader
}

// SourceDocument contains one collected source. Raw is redacted before Distiller receives it.
type SourceDocument struct {
	Provenance string
	Raw        string
}

// Build runs the two scanning barriers, mandatory review, signature, age encryption and upload in order.
func Build(ctx context.Context, in BuildInput, deps BuildDeps) error {
	if err := validateBuild(in, deps); err != nil {
		return err
	}
	draft, err := Prepare(ctx, in, deps.Distiller, deps.Scanner)
	if err != nil {
		return err
	}
	return Review(ctx, draft, in.PrivateKey, deps)
}

// Prepare collects no sources itself; it redacts before distillation and returns only reviewable sections.
func Prepare(ctx context.Context, in BuildInput, distiller Distiller, scanner Scanner) (Draft, error) {
	if in.Person == "" || in.Version < 1 || in.CreatedAt.IsZero() || in.Canary == "" || distiller == nil || scanner == nil {
		return Draft{}, errors.New("pack draft identity or dependencies are incomplete")
	}
	sections, removed, err := distillAndRedact(ctx, in.Documents, distiller, scanner, in.Canary)
	if err != nil {
		return Draft{}, err
	}
	return Draft{Person: in.Person, Version: in.Version, CreatedAt: in.CreatedAt.UTC(), TranscriptDays: in.TranscriptDays, Canary: in.Canary, Sections: sections, Removed: removed}, nil
}

// Review displays the already-redacted draft to the owner, then signs, encrypts and uploads only after approval.
func Review(ctx context.Context, draft Draft, privateKey ed25519.PrivateKey, deps BuildDeps) error {
	if err := validateReview(draft, privateKey, deps); err != nil {
		return err
	}
	manifest, plain, err := signPack(BuildInput{Person: draft.Person, Version: draft.Version, CreatedAt: draft.CreatedAt, TranscriptDays: draft.TranscriptDays, Canary: draft.Canary, PrivateKey: privateKey}, draft.Sections)
	if err != nil {
		return err
	}
	return approveEncryptUpload(ctx, draft.Person, plain, manifest, draft.Sections, draft.Removed, deps)
}

func validateReview(draft Draft, privateKey ed25519.PrivateKey, deps BuildDeps) error {
	if deps.Review == nil || deps.Cipher == nil || deps.Upload == nil || draft.Person == "" || draft.Version < 1 || draft.CreatedAt.IsZero() || draft.Canary == "" || len(privateKey) != ed25519.PrivateKeySize {
		return errors.New("pack review identity or dependencies are incomplete")
	}
	for i, section := range draft.Sections {
		if section.Name == "" || section.Content == "" {
			return fmt.Errorf("pack draft section %d is incomplete", i)
		}
	}
	return nil
}

func validateBuild(in BuildInput, deps BuildDeps) error {
	if deps.Distiller == nil || deps.Scanner == nil || deps.Review == nil || deps.Cipher == nil || deps.Upload == nil {
		return errors.New("pack build dependencies must not be nil")
	}
	if in.Person == "" || in.Version < 1 || in.CreatedAt.IsZero() || in.Canary == "" || len(in.PrivateKey) != ed25519.PrivateKeySize {
		return errors.New("pack build identity is incomplete")
	}
	return nil
}

func distillAndRedact(ctx context.Context, documents []SourceDocument, distiller Distiller, scanner Scanner, canary string) ([]Section, []Redaction, error) {
	cleanDocs := make([]SourceDocument, len(documents))
	var removed []Redaction
	for i, doc := range documents {
		clean, hits, err := scanner.Scan(doc.Raw)
		if err != nil {
			return nil, nil, fmt.Errorf("redact source %q: %w", doc.Provenance, err)
		}
		cleanDocs[i] = SourceDocument{Provenance: doc.Provenance, Raw: clean}
		removed = append(removed, hits...)
	}
	sections, err := distiller.Distill(ctx, cleanDocs)
	if err != nil {
		return nil, nil, fmt.Errorf("distill pack: %w", err)
	}
	for i := range sections {
		if sections[i].Name == "" || sections[i].Content == "" {
			return nil, nil, fmt.Errorf("pack section %d is incomplete", i)
		}
		clean, hits, err := scanner.Scan(sections[i].Content)
		if err != nil {
			return nil, nil, fmt.Errorf("scan pack section %q: %w", sections[i].Name, err)
		}
		if len(hits) != 0 || clean != sections[i].Content {
			return nil, nil, fmt.Errorf("redact pack section %q: second-pass scan found sensitive material", sections[i].Name)
		}
		sections[i].Content = insertCanary(sections[i].Content, canary)
	}
	return sections, removed, nil
}

func approveEncryptUpload(ctx context.Context, person string, plain []byte, manifest Manifest, sections []Section, removed []Redaction, deps BuildDeps) error {
	approved, err := deps.Review.Approve(ctx, manifest, sections, removed)
	if err != nil {
		return fmt.Errorf("review pack: %w", err)
	}
	if !approved {
		return errors.New("pack upload requires owner approval")
	}
	ciphertext, err := deps.Cipher.Encrypt(ctx, plain)
	if err != nil {
		return fmt.Errorf("encrypt pack with age: %w", err)
	}
	if len(ciphertext) == 0 {
		return errors.New("age encryption returned empty ciphertext")
	}
	if err := deps.Upload.Upload(ctx, person, ciphertext, manifest); err != nil {
		return fmt.Errorf("upload encrypted pack: %w", err)
	}
	return nil
}

func insertCanary(content, canary string) string { return content + "\n<!-- " + canary + " -->\n" }

func signPack(in BuildInput, sections []Section) (Manifest, []byte, error) {
	hashes := make(map[string]string, len(sections))
	for _, section := range sections {
		sum := sha256.Sum256([]byte(section.Content))
		hashes[section.Name] = hex.EncodeToString(sum[:])
	}
	manifest := Manifest{Person: in.Person, Version: in.Version, CreatedAt: in.CreatedAt.UTC(), TranscriptDays: in.TranscriptDays, SourceSummary: []string{"local source documents"}, ScannerVersions: map[string]string{"heimdall": "1"}, FileHashes: hashes, Canary: in.Canary}
	signed, err := json.Marshal(manifest)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("encode pack manifest: %w", err)
	}
	manifest.Signature = hex.EncodeToString(ed25519.Sign(in.PrivateKey, signed))
	payload, err := json.Marshal(struct {
		Manifest Manifest  `json:"manifest"`
		Sections []Section `json:"sections"`
	}{manifest, sections})
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("encode pack: %w", err)
	}
	return manifest, payload, nil
}
