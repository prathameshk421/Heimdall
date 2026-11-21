package ports

import (
	"context"
	"errors"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// The ports of Action mode (ACTION_MODE 2, 5.1, 6.1, work package AM9). Each is split by consumer and has at most
// five methods. Every text field they carry that came from GitHub is untrusted.

// ErrArtifactRefused means a downloaded artifact broke a limit or a safety rule: it is larger than the cap, holds too
// many entries, holds an entry whose path escapes, or is not a valid zip. The artifact is untrusted input, so the
// caller treats this as "unknown", never as a network failure to retry.
var ErrArtifactRefused = errors.New("artifact refused")

// ReviewComment is one inline review comment on a pull request. Line, StartLine and OriginalLine are 0 when GitHub
// reports null (a comment on a file, or one whose line is outdated). InReplyTo is 0 for a thread root.
type ReviewComment struct {
	ID           int64
	Login        string
	UserType     string
	Body         string
	Path         string
	Line         int
	StartLine    int
	OriginalLine int
	Side         string
	CommitID     string
	InReplyTo    int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// PRReviewCommentReader reads inline review comments. Consumer: prove-or-drop for third-party comments (ACTION_MODE 6).
type PRReviewCommentReader interface {
	// PRReviewComments returns every review comment of pull request n, across all pages.
	PRReviewComments(ctx context.Context, repo change.Repo, n int) ([]ReviewComment, error)
}

// WorkflowArtifact is one artifact a workflow run uploaded. HeadSHA is empty when GitHub does not name it.
type WorkflowArtifact struct {
	ID        int64
	Name      string
	SizeBytes int64
	Expired   bool
	CreatedAt time.Time
	RunID     int64
	HeadSHA   id.SHA
}

// ArtifactFile is one regular file of a downloaded artifact. Path is relative and clean.
type ArtifactFile struct {
	Path    string
	Content []byte
}

// ArtifactLimits bounds an untrusted artifact. Every field must be positive.
type ArtifactLimits struct {
	MaxZipBytes   int64 // the compressed download, refused while it streams
	MaxEntries    int   // the number of zip entries, directories included
	MaxFileBytes  int64 // one entry, uncompressed
	MaxTotalBytes int64 // all entries, uncompressed
}

// Validate returns every limit that is not positive.
func (l ArtifactLimits) Validate() error {
	var v problems
	v.require("max_zip_bytes", l.MaxZipBytes > 0, "must be positive")
	v.require("max_entries", l.MaxEntries > 0, "must be positive")
	v.require("max_file_bytes", l.MaxFileBytes > 0, "must be positive")
	v.require("max_total_bytes", l.MaxTotalBytes > 0, "must be positive")
	return v.join()
}

// ArtifactReader lists and downloads workflow artifacts. Consumer: the gate (proof artifact, coverage and SARIF inputs).
type ArtifactReader interface {
	// RunArtifacts lists the artifacts of one workflow run, across all pages.
	RunArtifacts(ctx context.Context, repo change.Repo, runID int64) ([]WorkflowArtifact, error)
	// DownloadArtifact downloads one artifact and returns its regular files. It returns ErrArtifactRefused when the
	// zip breaks a limit or has an unsafe path.
	DownloadArtifact(ctx context.Context, repo change.Repo, artifactID int64, limits ArtifactLimits) ([]ArtifactFile, error)
}

// CodeScanAlert is one code scanning alert. Description and Message come from the customer's analyzer and are
// untrusted. Line numbers are 0 when the alert has no location.
type CodeScanAlert struct {
	Number           int
	State            string
	RuleID           string
	Severity         string // the rule severity: none, note, warning or error
	SecuritySeverity string // low, medium, high or critical, or empty for a non-security rule
	Tool             string
	Description      string
	Message          string
	Path             string
	StartLine        int
	EndLine          int
	Ref              string
	CommitSHA        id.SHA
}

// CodeScanReader reads code scanning alerts. Consumer: the fast check's analyzers clause (ACTION_MODE 5.1).
type CodeScanReader interface {
	// CodeScanAlerts returns the alerts GitHub holds for ref (a full ref such as refs/pull/7/merge), across all pages.
	// state is empty, open, closed, fixed or dismissed; empty applies no filter.
	CodeScanAlerts(ctx context.Context, repo change.Repo, ref, state string) ([]CodeScanAlert, error)
}

// GitTreeEntry is one entry of a tree to write. Mode is 100644, 100755 or 040000 (a subtree).
type GitTreeEntry struct {
	Path string
	Mode string
	SHA  id.SHA
}

// Validate returns every problem with the entry.
func (e GitTreeEntry) Validate() error {
	var v problems
	v.add("path", checkRelPath(e.Path))
	switch e.Mode {
	case "100644", "100755", "040000":
	default:
		v.require("mode", false, "must be 100644, 100755 or 040000")
	}
	v.add("sha", checkSHA(e.SHA))
	return v.join()
}

// GitRefs reads and moves git refs through the Git Data API. These are raw operations. They are not a
// compare-and-swap: see UpdateRef.
type GitRefs interface {
	// GetRef returns the commit a full ref (refs/heads/x, refs/heimdall/state) points at. A missing ref is ErrNotFound.
	GetRef(ctx context.Context, repo change.Repo, ref string) (id.SHA, error)
	// CreateRef creates ref at sha. A ref that exists makes GitHub refuse the call, and the error is not typed.
	CreateRef(ctx context.Context, repo change.Repo, ref string, sha id.SHA) error
	// UpdateRef moves ref to sha. With force false GitHub refuses a non-fast-forward move on a branch. On a
	// non-branch ref such as refs/heimdall/* GitHub does NOT enforce fast-forward (live spike S1, to be confirmed),
	// so a nil error is not a compare-and-swap. The caller owns the conflict check.
	UpdateRef(ctx context.Context, repo change.Repo, ref string, sha id.SHA, force bool) error
}

// GitObjects writes git objects through the Git Data API. Consumer: the git-ref event store (AM8).
type GitObjects interface {
	// CreateBlob stores content and returns the blob's hash.
	CreateBlob(ctx context.Context, repo change.Repo, content []byte) (id.SHA, error)
	// CreateTree writes a tree of entries on top of base. An empty base makes a tree with only the entries.
	CreateTree(ctx context.Context, repo change.Repo, base id.SHA, entries []GitTreeEntry) (id.SHA, error)
	// CreateCommit writes a commit of tree with the given parents and returns its hash. It moves no ref.
	CreateCommit(ctx context.Context, repo change.Repo, message string, tree id.SHA, parents []id.SHA) (id.SHA, error)
}
