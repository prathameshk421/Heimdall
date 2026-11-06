package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/pack"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

var errPackNeedsApproval = errors.New("pack upload was not approved")

type localPackProfile struct {
	SecretsDir          string   `toml:"secrets_dir"`
	Person              string   `toml:"person"`
	RepositoryRoot      string   `toml:"repository_root"`
	MarkdownPaths       []string `toml:"markdown_paths"`
	GitAuthor           string   `toml:"git_author"`
	MaxCommits          int      `toml:"max_commits"`
	ClaudeSessionDirs   []string `toml:"claude_session_dirs"`
	CodexSessionDirs    []string `toml:"codex_session_dirs"`
	OpenCodeStorageRoot string   `toml:"opencode_storage_root"`
	TranscriptDays      int      `toml:"transcript_days"`
	ExtractionModel     string   `toml:"extraction_model"`
	OpenRouterKeySecret string   `toml:"openrouter_key_secret"`
	SigningKeySecret    string   `toml:"signing_private_key_secret"`
	AgeRecipientSecret  string   `toml:"age_recipient_secret"`
	UploadURL           string   `toml:"upload_url"`
	UploadTokenSecret   string   `toml:"upload_token_secret"`
	DraftPath           string   `toml:"draft_path"`
}

type localPackRuntime struct{}

func (localPackRuntime) Run(ctx context.Context, command, profilePath string, in io.Reader, out io.Writer) error {
	profile, err := loadPackProfile(profilePath)
	if err != nil {
		return err
	}
	draftPath := profile.DraftPath
	if draftPath == "" {
		draftPath = filepath.Join(profile.RepositoryRoot, ".heimdall", "pack-draft.json")
	}
	secrets := config.Config{SecretsDir: profile.SecretsDir}
	switch command {
	case "refresh":
		return refreshPack(ctx, profile, secrets, draftPath, out)
	case "review":
		return reviewPack(ctx, profile, secrets, draftPath, in, out)
	default:
		return fmt.Errorf("unknown pack command %q", command)
	}
}

func loadPackProfile(path string) (localPackProfile, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return localPackProfile{}, fmt.Errorf("open pack profile directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	content, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		return localPackProfile{}, fmt.Errorf("read pack profile %q: %w", path, err)
	}
	var profile localPackProfile
	if err := toml.NewDecoder(bytes.NewReader(content)).DisallowUnknownFields().Decode(&profile); err != nil {
		return localPackProfile{}, fmt.Errorf("decode pack profile %q: %w", path, err)
	}
	return profile, validatePackProfile(profile)
}

func validatePackProfile(p localPackProfile) error {
	if err := validatePackRequiredFields(p); err != nil {
		return err
	}
	if err := validatePackLimits(p); err != nil {
		return err
	}
	return validatePackUploadURL(p.UploadURL)
}

func validatePackRequiredFields(p localPackProfile) error {
	fields := []struct{ key, value string }{
		{"secrets_dir", p.SecretsDir}, {"person", p.Person}, {"repository_root", p.RepositoryRoot},
		{"extraction_model", p.ExtractionModel}, {"openrouter_key_secret", p.OpenRouterKeySecret},
		{"signing_private_key_secret", p.SigningKeySecret}, {"age_recipient_secret", p.AgeRecipientSecret},
		{"upload_url", p.UploadURL}, {"upload_token_secret", p.UploadTokenSecret},
	}
	for _, field := range fields {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("pack profile is missing required field %q", field.key)
		}
	}
	return nil
}

func validatePackLimits(p localPackProfile) error {
	if p.TranscriptDays < 0 || p.TranscriptDays > 3650 || p.MaxCommits < 0 || p.MaxCommits > 500 {
		return errors.New("pack profile transcript_days and max_commits are outside their allowed bounds")
	}
	if p.GitAuthor != "" && p.MaxCommits < 1 {
		return errors.New("pack profile max_commits is required when git_author is set")
	}
	return nil
}

func validatePackUploadURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("pack profile upload_url must be HTTPS without credentials, query, or fragment")
	}
	return nil
}

func refreshPack(ctx context.Context, p localPackProfile, secrets config.Config, draftPath string, out io.Writer) error {
	docs, err := collectPackSources(ctx, p)
	if err != nil {
		return err
	}
	token, err := secrets.Secret(p.OpenRouterKeySecret)
	if err != nil {
		return fmt.Errorf("pack profile openrouter_key_secret: %w", err)
	}
	scanner, err := pack.NewSecretScanner()
	if err != nil {
		return fmt.Errorf("build pack secret scanner: %w", err)
	}
	canary, err := newPackCanary()
	if err != nil {
		return fmt.Errorf("create pack canary: %w", err)
	}
	now := time.Now().UTC()
	input := pack.BuildInput{Person: p.Person, Version: int(now.UnixNano()), CreatedAt: now, TranscriptDays: p.TranscriptDays, Documents: docs, Canary: canary}
	draft, err := pack.Prepare(ctx, input, openRouterDistiller{client: packHTTPClient(), token: token, model: p.ExtractionModel}, scanner)
	if err != nil {
		return err
	}
	if err := pack.SaveDraft(draftPath, draft); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Saved redacted pack draft for %s at %s (%d sections, %d redaction groups removed). Run heimdall pack review to inspect and approve it.\n", p.Person, draftPath, len(draft.Sections), len(draft.Removed))
	return err
}

func collectPackSources(ctx context.Context, p localPackProfile) ([]pack.SourceDocument, error) {
	docs, err := pack.CollectMarkdown(p.RepositoryRoot, p.MarkdownPaths)
	if err != nil {
		return nil, err
	}
	if p.GitAuthor != "" {
		more, err := pack.CollectGit(ctx, p.RepositoryRoot, p.GitAuthor, p.MaxCommits)
		if err != nil {
			return nil, err
		}
		docs = append(docs, more...)
	}
	claude, err := pack.CollectClaude(ctx, p.RepositoryRoot, p.ClaudeSessionDirs)
	if err != nil {
		return nil, err
	}
	codex, err := pack.CollectCodex(ctx, p.RepositoryRoot, p.CodexSessionDirs)
	if err != nil {
		return nil, err
	}
	docs = append(docs, claude...)
	docs = append(docs, codex...)
	if p.OpenCodeStorageRoot != "" {
		more, err := pack.CollectOpenCode(ctx, p.OpenCodeStorageRoot, p.RepositoryRoot)
		if err != nil {
			return nil, err
		}
		docs = append(docs, more...)
	}
	if len(docs) == 0 {
		return nil, errors.New("pack profile selected no local source material")
	}
	return docs, nil
}

func reviewPack(ctx context.Context, p localPackProfile, secrets config.Config, draftPath string, in io.Reader, out io.Writer) error {
	draft, err := pack.LoadDraft(draftPath)
	if err != nil {
		return err
	}
	if draft.Person != p.Person {
		return errors.New("pack draft owner does not match the local profile")
	}
	privateKey, err := readPackSigningKey(secrets, p.SigningKeySecret)
	if err != nil {
		return err
	}
	recipient, err := secrets.Secret(p.AgeRecipientSecret)
	if err != nil {
		return fmt.Errorf("pack profile age_recipient_secret: %w", err)
	}
	token, err := secrets.Secret(p.UploadTokenSecret)
	if err != nil {
		return fmt.Errorf("pack profile upload_token_secret: %w", err)
	}
	return pack.Review(ctx, draft, privateKey, pack.BuildDeps{
		Review: terminalPackReview{input: in, output: out},
		Cipher: pack.AgeCipher{Recipient: recipient},
		Upload: httpPackUploader{client: packHTTPClient(), token: token, endpoint: p.UploadURL},
	})
}

func readPackSigningKey(secrets config.Config, name string) (ed25519.PrivateKey, error) {
	raw, err := secrets.SecretMultiline(name)
	if err != nil {
		return nil, fmt.Errorf("pack profile signing_private_key_secret: %w", err)
	}
	value := strings.TrimSpace(string(raw))
	decoded, err := hex.DecodeString(value)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(value)
	}
	if err != nil {
		return nil, errors.New("pack signing key must be hex or base64")
	}
	if len(decoded) == ed25519.SeedSize {
		return ed25519.NewKeyFromSeed(decoded), nil
	}
	if len(decoded) == ed25519.PrivateKeySize {
		return ed25519.PrivateKey(decoded), nil
	}
	return nil, errors.New("pack signing key must be a 32-byte seed or a 64-byte Ed25519 private key")
}

func newPackCanary() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "heimdall-private-" + hex.EncodeToString(raw[:]), nil
}

func packHTTPClient() *http.Client { return &http.Client{Timeout: 90 * time.Second} }

type terminalPackReview struct {
	input  io.Reader
	output io.Writer
}

func (r terminalPackReview) Approve(_ context.Context, manifest pack.Manifest, sections []pack.Section, removed []pack.Redaction) (bool, error) {
	if _, err := fmt.Fprintf(r.output, "Pack %s v%d. Removed-material summary: %v\n", manifest.Person, manifest.Version, removed); err != nil {
		return false, err
	}
	for _, section := range sections {
		if _, err := fmt.Fprintf(r.output, "\n## %s\n\n%s\n", section.Name, section.Content); err != nil {
			return false, err
		}
	}
	if _, err := io.WriteString(r.output, "\nType approve to sign, age-encrypt, and upload: "); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(r.input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read owner approval: %w", err)
	}
	if strings.TrimSpace(line) != "approve" {
		return false, errPackNeedsApproval
	}
	return true, nil
}

type openRouterDistiller struct {
	client *http.Client
	token  string
	model  string
}

func (d openRouterDistiller) Distill(ctx context.Context, docs []pack.SourceDocument) ([]pack.Section, error) {
	content, err := json.Marshal(docs)
	if err != nil {
		return nil, fmt.Errorf("encode redacted pack sources: %w", err)
	}
	request, err := json.Marshal(map[string]any{
		"model": d.model, "response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": "Create concise personal engineering context as JSON with a sections array. Preserve provenance for each useful statement, include only claims grounded in the supplied sources, and omit secrets or personal identifiers."},
			{"role": "user", "content": string(content)},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode pack extraction request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(request))
	if err != nil {
		return nil, fmt.Errorf("build pack extraction request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call configured pack extraction model: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read pack extraction response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pack extraction model returned HTTP %d", resp.StatusCode)
	}
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || len(response.Choices) == 0 {
		return nil, errors.New("pack extraction model returned an invalid response")
	}
	var result struct {
		Sections []pack.Section `json:"sections"`
	}
	if err := json.Unmarshal([]byte(response.Choices[0].Message.Content), &result); err != nil {
		return nil, fmt.Errorf("decode pack extraction sections: %w", err)
	}
	return result.Sections, nil
}

type httpPackUploader struct {
	client   *http.Client
	token    string
	endpoint string
}

func (u httpPackUploader) Upload(ctx context.Context, _ string, ciphertext []byte, manifest pack.Manifest) error {
	body, err := json.Marshal(map[string]any{"manifest": manifest, "ciphertext": ciphertext})
	if err != nil {
		return fmt.Errorf("encode encrypted pack upload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build encrypted pack upload: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+u.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("upload encrypted pack: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("read encrypted pack upload response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("encrypted pack upload returned HTTP %d", resp.StatusCode)
	}
	return nil
}
