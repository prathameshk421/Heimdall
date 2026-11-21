package github

import (
	"context"
	"fmt"
	"net/url"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// codeScanAlertJSON is the part of a code scanning alert the adapter reads. A null line decodes to 0.
type codeScanAlertJSON struct {
	Number int    `json:"number"`
	State  string `json:"state"`
	Rule   struct {
		ID                    string `json:"id"`
		Severity              string `json:"severity"`
		SecuritySeverityLevel string `json:"security_severity_level"`
		Description           string `json:"description"`
	} `json:"rule"`
	Tool struct {
		Name string `json:"name"`
	} `json:"tool"`
	MostRecentInstance struct {
		Ref       string `json:"ref"`
		CommitSHA string `json:"commit_sha"`
		Message   struct {
			Text string `json:"text"`
		} `json:"message"`
		Location struct {
			Path      string `json:"path"`
			StartLine int    `json:"start_line"`
			EndLine   int    `json:"end_line"`
		} `json:"location"`
	} `json:"most_recent_instance"`
}

// checkRef refuses a full ref that git would refuse or that could change the URL it is put in. It needs the refs/
// prefix and at least two more segments, so refs/heimdall/state and refs/heads/main pass and refs/x does not.
func checkRef(ref string) error {
	segs, err := cleanPath(ref)
	if err != nil {
		return err
	}
	if len(segs) < 3 || segs[0] != "refs" {
		return fmt.Errorf("github: ref %q must be a full ref such as refs/heads/main", ref)
	}
	return checkBranch(ref)
}

// CodeScanAlerts returns the code scanning alerts GitHub holds for ref across all pages. ref is a full ref, for
// example refs/pull/7/merge or refs/heads/main. state is empty (no filter), open, closed, fixed or dismissed.
// A repository without code scanning answers 404 or 403, which stay ports.ErrNotFound and ports.ErrUnauthorized; the
// caller decides whether that means "not checked". The rule description and the message come from the analyzer and
// are untrusted text.
func (x *ActionAPI) CodeScanAlerts(ctx context.Context, repo change.Repo, ref, state string) ([]ports.CodeScanAlert, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	if err := checkRef(ref); err != nil {
		return nil, err
	}
	switch state {
	case "", "open", "closed", "fixed", "dismissed":
	default:
		return nil, fmt.Errorf("github: code scanning state %q is not allowed", state)
	}
	token, err := x.tokens.Reader(ctx)
	if err != nil {
		return nil, err
	}
	query := url.Values{"ref": {ref}, "per_page": {perPage}}
	if state != "" {
		query.Set("state", state)
	}
	pg, err := listAll[codeScanAlertJSON](ctx, x.c, token, base+"/code-scanning/alerts", query, "")
	if err != nil {
		return nil, err
	}
	out := make([]ports.CodeScanAlert, 0, len(pg.items))
	for _, a := range pg.items {
		alert := ports.CodeScanAlert{
			Number: a.Number, State: a.State,
			RuleID: a.Rule.ID, Severity: a.Rule.Severity, SecuritySeverity: a.Rule.SecuritySeverityLevel,
			Tool: a.Tool.Name, Description: a.Rule.Description,
			Message: a.MostRecentInstance.Message.Text,
			Path:    a.MostRecentInstance.Location.Path, StartLine: a.MostRecentInstance.Location.StartLine,
			EndLine: a.MostRecentInstance.Location.EndLine,
			Ref:     a.MostRecentInstance.Ref,
		}
		if a.MostRecentInstance.CommitSHA != "" {
			sha, err := id.ParseSHA(a.MostRecentInstance.CommitSHA)
			if err != nil {
				return nil, fmt.Errorf("github: code scanning alert %d: %w", a.Number, err)
			}
			alert.CommitSHA = sha
		}
		out = append(out, alert)
	}
	return out, nil
}
