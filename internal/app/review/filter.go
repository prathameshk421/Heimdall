package review

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ErrOutputFiltered is returned for every output safety refusal, without exposing the detected value.
var ErrOutputFiltered = errors.New("output rejected")

// OutputAudit receives a private, non-sensitive reason for a refusal.
type OutputAudit interface{ RejectedOutput(context.Context, string) }

// OutputFilter applies the server-side limit, canary scan and secret scan to progress text.
type OutputFilter struct {
	canaries []string
	audit    OutputAudit
}

// NewOutputFilter constructs a filter with a copied canary list and a required audit sink.
func NewOutputFilter(canaries []string, audit OutputAudit) (OutputFilter, error) {
	if audit == nil {
		return OutputFilter{}, errors.New("output audit must not be nil")
	}
	return OutputFilter{canaries: append([]string(nil), canaries...), audit: audit}, nil
}

// Filter returns safe text unchanged, or the generic refusal for a blocked message.
func (f OutputFilter) Filter(ctx context.Context, text string) (string, error) {
	cause := outputCause(text, f.canaries)
	if cause == "" {
		return text, nil
	}
	f.audit.RejectedOutput(ctx, cause)
	return "", ErrOutputFiltered
}

func outputCause(text string, canaries []string) string {
	if utf8.RuneCountInString(text) > 400 {
		return "text exceeds 400 characters"
	}
	for _, canary := range canaries {
		if canary != "" && strings.Contains(text, canary) {
			return "canary detected"
		}
	}
	secret, err := regexp.MatchString(`(?i)(?:\bghp_[a-z0-9]{36}\b|\bgithub_pat_[a-z0-9_]{20,}\b|\bsk-[a-z0-9]{20,}\b|\bAKIA[0-9A-Z]{16}\b|\bBearer\s+[a-z0-9._-]{20,}\b|\b(?:api[_-]?key|token|secret|password)\s*[:=]\s*\S+|-----BEGIN [A-Z ]*PRIVATE KEY-----)`, text)
	if err != nil {
		return "secret pattern check failed"
	}
	if secret {
		return "secret pattern detected"
	}
	return ""
}
