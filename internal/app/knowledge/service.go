package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	// MaxFileBytes bounds one accepted source document.
	MaxFileBytes = 1 << 20
	// MaxPathBytes bounds the recorded relative source path.
	MaxPathBytes = 512
)

var (
	// ErrInvalidInput identifies malformed or out-of-bounds intake requests.
	ErrInvalidInput = errors.New("invalid knowledge intake")
	// ErrReplay identifies reuse of a key for different source bytes.
	ErrReplay = errors.New("idempotency key reused with different content")
	// ErrForbidden identifies access outside the authenticated identity's team.
	ErrForbidden = errors.New("knowledge access forbidden")
)

// Identity is resolved from an authenticated credential by Authenticator.
type Identity struct{ Subject, Team, Role string }

// Authenticator resolves a credential and verifies an owner approval reference.
type Authenticator interface {
	Authenticate(context.Context, string) (Identity, error)
}

// OwnerApprover resolves an approval reference to the authenticated owner who approved it.
type OwnerApprover interface {
	VerifyApproval(context.Context, string, string) (Identity, error)
}

// Redactor removes sensitive content or rejects content it cannot safely redact.
type Redactor interface {
	Redact(context.Context, string) (string, error)
}

// Record is the durable provenance and replay record for one approved source file.
type Record = ports.KnowledgeRecord

// State records whether durable provenance awaits memory delivery or is complete.
type State = ports.KnowledgeState

const (
	// Pending is durable provenance awaiting memory delivery.
	Pending = ports.KnowledgePending
	// Completed is provenance whose memory delivery succeeded.
	Completed = ports.KnowledgeComplete
)

// Repository stores provenance and looks up a record by team and idempotency key.
type Repository = ports.KnowledgeRepository

// MemoryWriter accepts already-redacted content and a server-selected team group.
type MemoryWriter = ports.KnowledgeWriter

// MemoryReader searches only the supplied server-selected team group.
type MemoryReader = ports.KnowledgeReader

// IntakeRequest carries untrusted file bytes, an opaque credential and an approval reference.
type IntakeRequest struct {
	Credential     string
	ApprovalRef    string
	Team           string
	SourcePath     string
	Content        []byte
	IdempotencyKey string
}

// ReadRequest carries an authenticated query; scope always comes from resolved identity.
type ReadRequest struct{ Credential, Team, Question string }

// Service coordinates safe intake and team-scoped reads.
type Service struct {
	auth       Authenticator
	approver   OwnerApprover
	redactor   Redactor
	repository Repository
	writer     MemoryWriter
	reader     MemoryReader
}

// New constructs the service from required production seams.
func New(auth Authenticator, approver OwnerApprover, redactor Redactor, repository Repository, writer MemoryWriter, reader MemoryReader) (*Service, error) {
	if auth == nil || approver == nil || redactor == nil || repository == nil || writer == nil || reader == nil {
		return nil, fmt.Errorf("knowledge service dependencies: %w", ErrInvalidInput)
	}
	return &Service{auth: auth, approver: approver, redactor: redactor, repository: repository, writer: writer, reader: reader}, nil
}

// Intake redacts approved bounded text before passing it to the memory writer.
func (s *Service) Intake(ctx context.Context, req IntakeRequest) (Record, error) {
	if err := validateIntake(req); err != nil {
		return Record{}, err
	}
	identity, approver, err := s.approvedIdentity(ctx, req)
	if err != nil {
		return Record{}, err
	}
	digest := sha256.Sum256(req.Content)
	sourceDigest := hex.EncodeToString(digest[:])
	prior, found, err := s.repository.Find(ctx, req.Team, req.IdempotencyKey)
	if err != nil {
		return Record{}, fmt.Errorf("find knowledge replay: %w", err)
	}
	if found {
		if prior.SourceDigest != sourceDigest {
			return Record{}, ErrReplay
		}
		if prior.State == Completed {
			return prior, nil
		}
		return s.deliver(ctx, prior)
	}
	return s.writeNew(ctx, req, identity, approver, sourceDigest)
}

func (s *Service) approvedIdentity(ctx context.Context, req IntakeRequest) (Identity, Identity, error) {
	identity, err := s.auth.Authenticate(ctx, req.Credential)
	if err != nil {
		return Identity{}, Identity{}, fmt.Errorf("authenticate knowledge intake: %w", err)
	}
	if identity.Subject == "" || identity.Team != req.Team {
		return Identity{}, Identity{}, ErrForbidden
	}
	approver, err := s.approver.VerifyApproval(ctx, req.ApprovalRef, req.Team)
	if err != nil {
		return Identity{}, Identity{}, fmt.Errorf("verify knowledge owner approval: %w", err)
	}
	if approver.Subject == "" || approver.Team != req.Team || approver.Role != "owner" {
		return Identity{}, Identity{}, ErrForbidden
	}
	return identity, approver, nil
}

func (s *Service) writeNew(ctx context.Context, req IntakeRequest, identity, approver Identity, sourceDigest string) (Record, error) {
	clean, err := s.redactor.Redact(ctx, string(req.Content))
	if err != nil {
		return Record{}, fmt.Errorf("redact knowledge content: %w", err)
	}
	if clean == "" || !utf8.ValidString(clean) || len(clean) > MaxFileBytes {
		return Record{}, ErrInvalidInput
	}
	cleanDigest := sha256.Sum256([]byte(clean))
	record := Record{Team: req.Team, SourcePath: req.SourcePath, SourceDigest: sourceDigest, ContentDigest: hex.EncodeToString(cleanDigest[:]), IdempotencyKey: req.IdempotencyKey, ApprovedBy: approver.Subject, Subject: identity.Subject, Content: clean, State: Pending}
	record, err = s.repository.Begin(ctx, record)
	if err != nil {
		return Record{}, fmt.Errorf("begin knowledge provenance: %w", err)
	}
	if record.SourceDigest != sourceDigest {
		return Record{}, ErrReplay
	}
	return s.deliver(ctx, record)
}

func (s *Service) deliver(ctx context.Context, record Record) (Record, error) {
	if err := s.writer.Write(ctx, record.Team, record.IdempotencyKey, record.Content); err != nil {
		return Record{}, fmt.Errorf("write team knowledge: %w", err)
	}
	if err := s.repository.Complete(ctx, record.Team, record.IdempotencyKey); err != nil {
		return Record{}, fmt.Errorf("complete knowledge provenance: %w", err)
	}
	record.State = Completed
	return record, nil
}

// Read authenticates the caller and fixes search scope to that caller's team.
func (s *Service) Read(ctx context.Context, req ReadRequest) ([]string, error) {
	if req.Team == "" || strings.TrimSpace(req.Question) == "" || len(req.Question) > 1024 {
		return nil, ErrInvalidInput
	}
	identity, err := s.auth.Authenticate(ctx, req.Credential)
	if err != nil {
		return nil, fmt.Errorf("authenticate knowledge read: %w", err)
	}
	if identity.Subject == "" || identity.Team != req.Team {
		return nil, ErrForbidden
	}
	results, err := s.reader.Search(ctx, identity.Team, req.Question)
	if err != nil {
		return nil, fmt.Errorf("search team knowledge: %w", err)
	}
	return results, nil
}

func validateIntake(req IntakeRequest) error {
	if hasMissingIntakeFields(req) || len(req.IdempotencyKey) > 256 {
		return ErrInvalidInput
	}
	if !safeSourcePath(req.SourcePath) {
		return ErrInvalidInput
	}
	if !supportedTextFile(req.SourcePath) || !validContent(req.Content) {
		return ErrInvalidInput
	}
	return nil
}

func hasMissingIntakeFields(req IntakeRequest) bool {
	return req.Credential == "" || req.ApprovalRef == "" || req.Team == "" || req.IdempotencyKey == ""
}

func safeSourcePath(source string) bool {
	return source != "" && len(source) <= MaxPathBytes && !strings.Contains(source, "\\") && !path.IsAbs(source) && path.Clean(source) == source && source != "." && !strings.HasPrefix(source, "../")
}

func supportedTextFile(source string) bool {
	ext := strings.ToLower(path.Ext(source))
	return ext == ".md" || ext == ".markdown" || ext == ".txt"
}

func validContent(content []byte) bool {
	return len(content) > 0 && len(content) <= MaxFileBytes && utf8.Valid(content)
}
