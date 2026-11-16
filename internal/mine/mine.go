package mine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// Defaults of REVIEW_ARCH 4.6. The caller may pass other limits from policy.
const (
	MaxFiles      = 30
	MinSupport    = 5
	MinConfidence = 0.8
)

// Commit is one commit of the base branch's history, oldest first.
type Commit struct {
	Hash    string   `json:"hash"`
	Parents []string `json:"parents"`
	Files   []string `json:"files"`
}

// Rule is A => B: when A changes, B usually changes too (support of the pair over the commits of A).
type Rule struct {
	Antecedent        string  `json:"antecedent"`
	Consequent        string  `json:"consequent"`
	Support           int     `json:"support"`
	AntecedentCommits int     `json:"antecedent_commits"`
	Confidence        float64 `json:"confidence"`
}

// Limits are the thresholds a rule must pass.
type Limits struct {
	MaxFiles      int
	MinSupport    int
	MinConfidence float64
}

// DefaultLimits are the 4.6 defaults.
func DefaultLimits() Limits {
	return Limits{MaxFiles: MaxFiles, MinSupport: MinSupport, MinConfidence: MinConfidence}
}

// Mine returns the rules that pass limits, sorted by antecedent then consequent.
func Mine(commits []Commit, lim Limits) []Rule {
	support := map[[2]string]int{}
	count := map[string]int{}
	for _, c := range commits {
		files := distinct(c.Files)
		if len(c.Parents) > 1 || len(files) > lim.MaxFiles {
			continue
		}
		for _, a := range files {
			count[a]++
			for _, b := range files {
				if a != b {
					support[[2]string{a, b}]++
				}
			}
		}
	}
	rules := []Rule{}
	for pair, s := range support {
		a := pair[0]
		conf := float64(s) / float64(count[a])
		if s < lim.MinSupport || conf < lim.MinConfidence {
			continue
		}
		rules = append(rules, Rule{Antecedent: a, Consequent: pair[1], Support: s, AntecedentCommits: count[a], Confidence: conf})
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Antecedent != rules[j].Antecedent {
			return rules[i].Antecedent < rules[j].Antecedent
		}
		return rules[i].Consequent < rules[j].Consequent
	})
	return rules
}

func distinct(files []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

// Companion is one lead: changed file A has rule A => B and B is not in the change.
type Companion struct {
	Rule       Rule
	Antecedent string
}

// Companions returns one lead per (changed A, rule A => B with B not changed), in the order of changed, then by
// consequent. rules must be sorted as Mine returns them.
func Companions(rules []Rule, changed []string) []Companion {
	inChange := map[string]bool{}
	for _, f := range changed {
		inChange[f] = true
	}
	var out []Companion
	for _, a := range changed {
		for _, r := range rules {
			if r.Antecedent == a && !inChange[r.Consequent] {
				out = append(out, Companion{Rule: r, Antecedent: a})
			}
		}
	}
	return out
}

// Message is the sentence of REVIEW_ARCH 4.6 with the numbers filled in.
func (c Companion) Message() string {
	return fmt.Sprintf("%s changed in %d of the last %d commits that changed %s. %s is not in this change.",
		c.Rule.Consequent, c.Rule.Support, c.Rule.AntecedentCommits, c.Antecedent, c.Rule.Consequent)
}

// encode writes v as the goldens are written: two-space indent, no HTML escaping, one newline at the end.
func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// RulesJSON is the rules.json golden's encoding of rules.
func RulesJSON(rules []Rule) ([]byte, error) { return encode(rules) }

// SARIFJSON is the sarif.json golden's encoding of the companions of one change.
func SARIFJSON(cs []Companion) ([]byte, error) {
	type loc struct {
		PhysicalLocation struct {
			ArtifactLocation struct {
				URI string `json:"uri"`
			} `json:"artifactLocation"`
		} `json:"physicalLocation"`
	}
	type result struct {
		RuleID  string `json:"ruleId"`
		Level   string `json:"level"`
		Message struct {
			Text string `json:"text"`
		} `json:"message"`
		Locations []loc `json:"locations"`
	}
	results := []result{}
	for _, c := range cs {
		var r result
		r.RuleID, r.Level = "forgotten-companion", "note"
		r.Message.Text = c.Message()
		var l loc
		l.PhysicalLocation.ArtifactLocation.URI = c.Antecedent
		r.Locations = []loc{l}
		results = append(results, r)
	}
	type driver struct {
		Name string `json:"name"`
	}
	type run struct {
		Tool struct {
			Driver driver `json:"driver"`
		} `json:"tool"`
		Results []result `json:"results"`
	}
	var rn run
	rn.Tool.Driver = driver{Name: "heimdall-mine"}
	rn.Results = results
	doc := struct {
		Schema  string `json:"$schema"`
		Version string `json:"version"`
		Runs    []run  `json:"runs"`
	}{"https://json.schemastore.org/sarif-2.1.0.json", "2.1.0", []run{rn}}
	return encode(doc)
}
