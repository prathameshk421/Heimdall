package sarif

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// MaxBytes bounds one SARIF document.
const MaxBytes = 8 << 20

// Result is one finding a check reported (REVIEW_ARCH 4.4). Check and Lens come from the profile row that ran the tool.
type Result struct {
	Check   string `json:"check"`
	Lens    string `json:"lens"`
	Rule    string `json:"rule"`
	Level   string `json:"level"`
	File    string `json:"file"`
	Lines   [2]int `json:"lines"`
	Message string `json:"message"`
	Finger  string `json:"finger"`
}

// The SARIF fields this package reads. Everything else in a document is ignored.
type doc struct {
	Runs []struct {
		Results []struct {
			RuleID  string `json:"ruleId"`
			Level   string `json:"level"`
			Message struct {
				Text string `json:"text"`
			} `json:"message"`
			Locations []struct {
				PhysicalLocation struct {
					ArtifactLocation struct {
						URI string `json:"uri"`
					} `json:"artifactLocation"`
					Region *struct {
						StartLine int `json:"startLine"`
						EndLine   int `json:"endLine"`
						Snippet   struct {
							Text string `json:"text"`
						} `json:"snippet"`
					} `json:"region"`
				} `json:"physicalLocation"`
			} `json:"locations"`
		} `json:"results"`
	} `json:"runs"`
}

// Parse reads one SARIF document written by the check row named check, whose lens is lens.
func Parse(data []byte, check, lens string) ([]Result, error) {
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("sarif: %d bytes, at most %d", len(data), MaxBytes)
	}
	var d doc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("sarif: %w", err)
	}
	out := []Result{}
	for _, run := range d.Runs {
		for i, r := range run.Results {
			res, err := toResult(r.RuleID, r.Level, r.Message.Text, r.Locations, check, lens)
			if err != nil {
				return nil, fmt.Errorf("sarif: result %d: %w", i, err)
			}
			out = append(out, res)
		}
	}
	return out, nil
}

func toResult(rule, level, msg string, locs []struct {
	PhysicalLocation struct {
		ArtifactLocation struct {
			URI string `json:"uri"`
		} `json:"artifactLocation"`
		Region *struct {
			StartLine int `json:"startLine"`
			EndLine   int `json:"endLine"`
			Snippet   struct {
				Text string `json:"text"`
			} `json:"snippet"`
		} `json:"region"`
	} `json:"physicalLocation"`
}, check, lens string) (Result, error) {
	if rule == "" {
		return Result{}, fmt.Errorf("no ruleId")
	}
	switch level {
	case "error", "warning", "note":
	default:
		return Result{}, fmt.Errorf("level %q is not error, warning or note", level)
	}
	res := Result{Check: check, Lens: lens, Rule: rule, Level: level, Message: msg}
	if len(locs) == 0 {
		res.Finger = rule + ":"
		return res, nil
	}
	pl := locs[0].PhysicalLocation
	res.File = pl.ArtifactLocation.URI
	if res.File == "" {
		return Result{}, fmt.Errorf("rule %s: no artifact uri", rule)
	}
	reg := pl.Region
	if reg == nil {
		res.Finger = rule + ":" + res.File + ":"
		return res, nil
	}
	if reg.StartLine < 1 || reg.EndLine < reg.StartLine {
		return Result{}, fmt.Errorf("rule %s in %s: lines %d-%d are not a valid range", rule, res.File, reg.StartLine, reg.EndLine)
	}
	if reg.Snippet.Text == "" {
		return Result{}, fmt.Errorf("rule %s in %s: region has no snippet text to fingerprint", rule, res.File)
	}
	res.Lines = [2]int{reg.StartLine, reg.EndLine}
	res.Finger = fingerprint(rule, res.File, reg.Snippet.Text)
	return res, nil
}

// fingerprint is rule:file:hex(sha256(collapse(text))). Collapsing white space makes re-indented code match.
func fingerprint(rule, file, text string) string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(text), " ")))
	return rule + ":" + file + ":" + hex.EncodeToString(sum[:])
}

// New returns the head results that no base result of the same check matches by fingerprint, in head order.
func New(base, head []Result) []Result {
	seen := make(map[string]bool, len(base))
	for _, r := range base {
		seen[r.Check+"\x00"+r.Finger] = true
	}
	out := []Result{}
	for _, r := range head {
		if !seen[r.Check+"\x00"+r.Finger] {
			out = append(out, r)
		}
	}
	return out
}
