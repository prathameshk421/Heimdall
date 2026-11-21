package github

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// artifactTimeout bounds one artifact download, redirect and body together. The byte cap bounds the memory.
const artifactTimeout = 5 * time.Minute

// artifactJSON is one artifact in a run's artifact list.
type artifactJSON struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	SizeInBytes int64     `json:"size_in_bytes"`
	Expired     bool      `json:"expired"`
	CreatedAt   time.Time `json:"created_at"`
	WorkflowRun struct {
		ID      int64  `json:"id"`
		HeadSHA string `json:"head_sha"`
	} `json:"workflow_run"`
}

// artifactsJSON is the list reply: the artifacts sit inside an object, not at the top.
type artifactsJSON struct {
	Artifacts []artifactJSON `json:"artifacts"`
}

// listAllIn collects every page of a list whose items sit inside a wrapper object.
func listAllIn[W, T any](ctx context.Context, c *client, auth, path string, query url.Values, items func(W) []T) ([]T, error) {
	var all []T
	req := request{method: http.MethodGet, path: path, query: query, auth: auth}
	for {
		resp, err := c.call(ctx, req)
		if err != nil {
			return nil, err
		}
		w, err := decode[W](resp, path)
		if err != nil {
			return nil, err
		}
		all = append(all, items(w)...)
		next, err := c.nextLink(resp.header.Get("Link"))
		if err != nil {
			return nil, err
		}
		if next == "" {
			return all, nil
		}
		req = request{method: http.MethodGet, path: path, auth: auth, target: next}
	}
}

// RunArtifacts lists the artifacts of one workflow run across all pages. An expired artifact is listed with Expired
// set, and a download of it fails.
func (x *ActionAPI) RunArtifacts(ctx context.Context, repo change.Repo, runID int64) ([]ports.WorkflowArtifact, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	if runID < 1 {
		return nil, fmt.Errorf("github: run id %d is not valid", runID)
	}
	token, err := x.tokens.Reader(ctx)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("%s/actions/runs/%d/artifacts", base, runID)
	items, err := listAllIn(ctx, x.c, token, path, url.Values{"per_page": {perPage}}, func(w artifactsJSON) []artifactJSON { return w.Artifacts })
	if err != nil {
		return nil, err
	}
	out := make([]ports.WorkflowArtifact, 0, len(items))
	for _, a := range items {
		art := ports.WorkflowArtifact{
			ID: a.ID, Name: a.Name, SizeBytes: a.SizeInBytes, Expired: a.Expired,
			CreatedAt: a.CreatedAt, RunID: a.WorkflowRun.ID,
		}
		if a.WorkflowRun.HeadSHA != "" {
			sha, err := id.ParseSHA(a.WorkflowRun.HeadSHA)
			if err != nil {
				return nil, fmt.Errorf("github: artifact %d: %w", a.ID, err)
			}
			art.HeadSHA = sha
		}
		out = append(out, art)
	}
	return out, nil
}

// DownloadArtifact downloads one artifact zip and returns its regular files. The artifact is untrusted input, so:
//   - The zip is read while it streams, and a zip larger than limits.MaxZipBytes is refused at the cap.
//   - The zip holds at most limits.MaxEntries entries.
//   - An entry path that is absolute, holds a backslash, a NUL, a drive letter, or a "." or ".." segment is refused.
//   - An entry that is a symbolic link or another irregular file is refused, and so is a repeated path.
//   - One entry is at most limits.MaxFileBytes and all entries are at most limits.MaxTotalBytes after expansion.
//     The size in a zip header can lie, so each entry is read through a limit and never trusted.
//
// Every refusal is ports.ErrArtifactRefused. The reason names an entry by its index, never by its path.
// GitHub answers with a redirect to a signed URL on another host. Go drops the Authorization header on a redirect
// to another host. Nothing was verified against a live run (S1).
func (x *ActionAPI) DownloadArtifact(ctx context.Context, repo change.Repo, artifactID int64, limits ports.ArtifactLimits) ([]ports.ArtifactFile, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	if artifactID < 1 {
		return nil, fmt.Errorf("github: artifact id %d is not valid", artifactID)
	}
	if err := limits.Validate(); err != nil {
		return nil, fmt.Errorf("github: artifact limits: %w", err)
	}
	token, err := x.tokens.Reader(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, artifactTimeout)
	defer cancel()
	body, err := x.c.open(ctx, request{method: http.MethodGet, path: fmt.Sprintf("%s/actions/artifacts/%d/zip", base, artifactID), auth: token})
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(body, limits.MaxZipBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: github artifact %d: read zip: %w", ports.ErrUnavailable, artifactID, err)
	}
	if int64(len(raw)) > limits.MaxZipBytes {
		return nil, refused("the zip is larger than %d bytes", limits.MaxZipBytes)
	}
	return unpackArtifact(raw, limits)
}

// refused builds a ports.ErrArtifactRefused with a reason.
func refused(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ports.ErrArtifactRefused, fmt.Sprintf(format, args...))
}

// unpackArtifact checks and reads a zip held in memory. It is pure, so a test feeds it any bytes.
func unpackArtifact(raw []byte, limits ports.ArtifactLimits) ([]ports.ArtifactFile, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		if errors.Is(err, zip.ErrInsecurePath) {
			return nil, refused("an entry path escapes the artifact")
		}
		return nil, refused("the file is not a valid zip")
	}
	if len(zr.File) > limits.MaxEntries {
		return nil, refused("the zip holds more than %d entries", limits.MaxEntries)
	}
	u := unpacker{limits: limits, seen: make(map[string]struct{}, len(zr.File))}
	var out []ports.ArtifactFile
	for i, f := range zr.File {
		file, keep, err := u.entry(i, f)
		if err != nil {
			return nil, err
		}
		if keep {
			out = append(out, file)
		}
	}
	return out, nil
}

// unpacker holds the state shared by the entries of one zip: the paths seen and the bytes read so far.
type unpacker struct {
	limits ports.ArtifactLimits
	seen   map[string]struct{}
	total  int64
}

// entry checks one zip entry and reads it. keep is false for a directory.
func (u *unpacker) entry(i int, f *zip.File) (file ports.ArtifactFile, keep bool, err error) {
	name, isDir, err := entryPath(f.Name)
	if err != nil {
		return file, false, refused("entry %d has a path that escapes or is not clean", i)
	}
	if _, dup := u.seen[name]; dup {
		return file, false, refused("entry %d repeats an earlier path", i)
	}
	u.seen[name] = struct{}{}
	mode := f.Mode()
	if isDir || mode.IsDir() {
		return file, false, nil
	}
	if !mode.IsRegular() || mode&os.ModeSymlink != 0 {
		return file, false, refused("entry %d is not a regular file", i)
	}
	content, err := readEntry(f, u.limits.MaxFileBytes)
	if err != nil {
		return file, false, err
	}
	u.total += int64(len(content))
	if u.total > u.limits.MaxTotalBytes {
		return file, false, refused("the entries are larger than %d bytes together", u.limits.MaxTotalBytes)
	}
	return ports.ArtifactFile{Path: name, Content: content}, true, nil
}

// readEntry reads one entry through a limit, whatever its header says.
func readEntry(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, refused("an entry cannot be opened")
	}
	defer func() { _ = rc.Close() }()
	content, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, refused("an entry is damaged")
	}
	if int64(len(content)) > limit {
		return nil, refused("an entry is larger than %d bytes", limit)
	}
	return content, nil
}

// entryPath cleans a zip entry name. A directory entry ends with a slash. It refuses a path that leaves the artifact:
// absolute, with a backslash or a NUL, with a drive letter, or with an empty, "." or ".." segment.
func entryPath(name string) (clean string, isDir bool, err error) {
	isDir = strings.HasSuffix(name, "/")
	trimmed := strings.TrimSuffix(name, "/")
	if len(trimmed) >= 2 && trimmed[1] == ':' {
		return "", false, errors.New("drive letter")
	}
	if _, err := cleanPath(trimmed); err != nil || trimmed == "" {
		return "", false, errors.New("unclean path")
	}
	return trimmed, isDir, nil
}
