package knowledge

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type authFake struct {
	identity Identity
	authErr  error
}

func (a *authFake) Authenticate(context.Context, string) (Identity, error) {
	return a.identity, a.authErr
}

type approverFake struct {
	identity Identity
	err      error
	calls    int
}

func (a *approverFake) VerifyApproval(context.Context, string, string) (Identity, error) {
	a.calls++
	return a.identity, a.err
}

type redactorFake struct {
	output string
	err    error
	calls  int
}

func (r *redactorFake) Redact(_ context.Context, input string) (string, error) {
	r.calls++
	if r.output != "" {
		return r.output, r.err
	}
	return input, r.err
}

type repoFake struct {
	records map[string]Record
	saves   int
}

func (r *repoFake) Find(_ context.Context, team, key string) (Record, bool, error) {
	rec, ok := r.records[team+"/"+key]
	return rec, ok, nil
}
func (r *repoFake) Begin(_ context.Context, rec Record) (Record, error) {
	if r.records == nil {
		r.records = make(map[string]Record)
	}
	key := rec.Team + "/" + rec.IdempotencyKey
	if prior, ok := r.records[key]; ok {
		return prior, nil
	}
	r.records[key] = rec
	r.saves++
	return rec, nil
}
func (r *repoFake) Complete(_ context.Context, team, key string) error {
	rec := r.records[team+"/"+key]
	rec.State = Completed
	r.records[team+"/"+key] = rec
	r.saves++
	return nil
}

type writerFake struct {
	team, key, content string
	calls              int
	redactor           *redactorFake
	err                error
}

func (w *writerFake) Write(_ context.Context, team, key, content string) error {
	w.team, w.key, w.content = team, key, content
	w.calls++
	return w.err
}

type readerFake struct {
	team, question string
	calls          int
}

func (r *readerFake) Search(_ context.Context, team, question string) ([]string, error) {
	r.team, r.question = team, question
	r.calls++
	return []string{"ref-1"}, nil
}

func newService(t *testing.T, identity Identity) (*Service, *approverFake, *redactorFake, *repoFake, *writerFake, *readerFake) {
	t.Helper()
	auth := &authFake{identity: identity}
	approver := &approverFake{identity: Identity{Subject: "owner-alice", Team: identity.Team, Role: "owner"}}
	redactor := &redactorFake{output: "clean knowledge"}
	repo := &repoFake{}
	writer := &writerFake{redactor: redactor}
	reader := &readerFake{}
	svc, err := New(auth, approver, redactor, repo, writer, reader)
	if err != nil {
		t.Fatal(err)
	}
	return svc, approver, redactor, repo, writer, reader
}

func goodRequest() IntakeRequest {
	return IntakeRequest{Credential: "opaque", ApprovalRef: "approval-1", Team: "payments", SourcePath: "docs/refunds.md", Content: []byte("raw with planted secret"), IdempotencyKey: "delivery-1"}
}

func TestIntakeRedactsBeforeMemoryWriteAndPersistsProvenance(t *testing.T) {
	svc, approver, redactor, repo, writer, _ := newService(t, Identity{Subject: "agent-1", Team: "payments", Role: "agent"})
	record, err := svc.Intake(context.Background(), goodRequest())
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	assertRedactedWrite(t, writer)
	assertKnowledgeProvenance(t, record, approver, redactor, repo)
}

func assertRedactedWrite(t *testing.T, writer *writerFake) {
	t.Helper()
	if writer.content != "clean knowledge" || writer.team != "payments" {
		t.Fatalf("writer got unredacted or wrong-scope input: %#v", writer)
	}
}

func assertKnowledgeProvenance(t *testing.T, record Record, approver *approverFake, redactor *redactorFake, repo *repoFake) {
	t.Helper()
	if record.SourcePath != "docs/refunds.md" || record.SourceDigest == "" || record.ContentDigest == "" || record.ApprovedBy != "owner-alice" || record.Subject != "agent-1" {
		t.Fatalf("provenance missing: %#v", record)
	}
	if approver.calls != 1 || redactor.calls != 1 || repo.saves != 2 || record.State != Completed {
		t.Fatalf("expected authenticated approval, redaction and durable save: %d %d %d", approver.calls, redactor.calls, repo.saves)
	}
}

func TestIntakeRejectsUnapprovedCrossTeamAndUnsafeFiles(t *testing.T) {
	t.Run("approval failure", func(t *testing.T) {
		svc, approver, _, _, writer, _ := newService(t, Identity{Subject: "agent-1", Team: "payments"})
		approver.err = ErrForbidden
		if _, err := svc.Intake(context.Background(), goodRequest()); err == nil {
			t.Fatal("unapproved intake succeeded")
		}
		if writer.calls != 0 {
			t.Fatal("memory writer called without owner approval")
		}
	})
	t.Run("cross-team", func(t *testing.T) {
		svc, _, _, _, writer, _ := newService(t, Identity{Subject: "alice", Team: "payments"})
		req := goodRequest()
		req.Team = "security"
		if _, err := svc.Intake(context.Background(), req); !errors.Is(err, ErrForbidden) {
			t.Fatalf("cross-team error = %v", err)
		}
		if writer.calls != 0 {
			t.Fatal("memory writer called for another team")
		}
	})
	for _, file := range []string{"../secret.md", "/absolute.md", "docs\\secret.md", "secrets.pdf", "nested/../../x.txt"} {
		t.Run(file, func(t *testing.T) {
			svc, _, _, _, writer, _ := newService(t, Identity{Subject: "alice", Team: "payments"})
			req := goodRequest()
			req.SourcePath = file
			if _, err := svc.Intake(context.Background(), req); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("unsafe file accepted: %v", err)
			}
			if writer.calls != 0 {
				t.Fatal("memory writer called for invalid file")
			}
		})
	}
}

func TestIntakeRejectsReplayContentMismatchAndDoesNotRewrite(t *testing.T) {
	svc, _, _, repo, writer, _ := newService(t, Identity{Subject: "alice", Team: "payments"})
	if _, err := svc.Intake(context.Background(), goodRequest()); err != nil {
		t.Fatal(err)
	}
	req := goodRequest()
	req.Content = []byte("different source")
	if _, err := svc.Intake(context.Background(), req); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay mismatch error = %v", err)
	}
	if writer.calls != 1 || repo.saves != 2 {
		t.Fatalf("mismatch caused duplicate write/save: %d/%d", writer.calls, repo.saves)
	}
}

func TestIntakeSameReplayIsIdempotent(t *testing.T) {
	svc, _, _, repo, writer, _ := newService(t, Identity{Subject: "alice", Team: "payments"})
	first, err := svc.Intake(context.Background(), goodRequest())
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Intake(context.Background(), goodRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || writer.calls != 1 || repo.saves != 2 {
		t.Fatalf("replay was not idempotent: calls %d/%d", writer.calls, repo.saves)
	}
}

func TestIntakeFailedMemoryWriteRemainsPendingAndReplayRetriesIdempotently(t *testing.T) {
	svc, _, _, repo, writer, _ := newService(t, Identity{Subject: "alice", Team: "payments"})
	writer.err = errors.New("graphiti unavailable")
	if _, err := svc.Intake(context.Background(), goodRequest()); err == nil {
		t.Fatal("failed memory write was reported as successful")
	}
	if got := repo.records["payments/delivery-1"].State; got != Pending {
		t.Fatalf("failed write state = %q, want pending", got)
	}
	writer.err = nil
	record, err := svc.Intake(context.Background(), goodRequest())
	if err != nil {
		t.Fatalf("retry Intake: %v", err)
	}
	if record.State != Completed || writer.calls != 2 || repo.records["payments/delivery-1"].State != Completed {
		t.Fatalf("retry did not complete once: record=%#v calls=%d stored=%q", record, writer.calls, repo.records["payments/delivery-1"].State)
	}
	if _, err := svc.Intake(context.Background(), goodRequest()); err != nil || writer.calls != 2 {
		t.Fatalf("completed replay re-delivered: err=%v calls=%d", err, writer.calls)
	}
}

type racingRepository struct{}

func (r *racingRepository) Find(context.Context, string, string) (Record, bool, error) {
	return Record{}, false, nil
}

func (r *racingRepository) Begin(_ context.Context, requested Record) (Record, error) {
	prior := requested
	prior.SourceDigest = "another-content-digest"
	return prior, nil
}

func (*racingRepository) Complete(context.Context, string, string) error { return nil }

func TestIntakeConcurrentReplayWithDifferentContentIsRejected(t *testing.T) {
	auth := &authFake{identity: Identity{Subject: "alice", Team: "payments"}}
	approver := &approverFake{identity: Identity{Subject: "owner", Team: "payments", Role: "owner"}}
	writer := &writerFake{}
	svc, err := New(auth, approver, &redactorFake{output: "clean"}, &racingRepository{}, writer, &readerFake{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Intake(context.Background(), goodRequest()); !errors.Is(err, ErrReplay) {
		t.Fatalf("concurrent replay error = %v, want ErrReplay", err)
	}
	if writer.calls != 0 {
		t.Fatalf("concurrent conflicting request reached writer %d times", writer.calls)
	}
}

func TestReadAuthenticatesAndUsesOnlyIdentityTeam(t *testing.T) {
	svc, _, _, _, _, reader := newService(t, Identity{Subject: "agent-1", Team: "payments", Role: "reviewer"})
	got, err := svc.Read(context.Background(), ReadRequest{Credential: "opaque", Team: "payments", Question: "refund invariant"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"ref-1"}) || reader.team != "payments" {
		t.Fatalf("read result or scope wrong: %#v %#v", got, reader)
	}
	if _, err := svc.Read(context.Background(), ReadRequest{Credential: "opaque", Team: "security", Question: "secret"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-team read error = %v", err)
	}
	if reader.calls != 1 {
		t.Fatalf("unauthorized query reached reader: %d calls", reader.calls)
	}
}
