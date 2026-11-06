package pack

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/viper"
	"github.com/zricethezav/gitleaks/v8/config"
	"github.com/zricethezav/gitleaks/v8/detect"
)

// SecretScanner embeds the upstream Gitleaks rules and applies Heimdall's additional PII rules.
type SecretScanner struct {
	detector *detect.Detector
	denied   []*regexp.Regexp
}

// ScannerConfig supplies deployment-specific internal hostnames and team deny-list terms.
type ScannerConfig struct {
	InternalHostnames []string
	DenyTerms         []string
}

// NewSecretScanner constructs a scanner without mutating Gitleaks' process-global Viper instance.
func NewSecretScanner() (*SecretScanner, error) {
	return NewSecretScannerWithConfig(ScannerConfig{})
}

// NewSecretScannerWithConfig builds the embedded upstream scanner and validated organization redaction rules.
func NewSecretScannerWithConfig(configured ScannerConfig) (*SecretScanner, error) {
	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(strings.NewReader(config.DefaultConfig)); err != nil {
		return nil, fmt.Errorf("read embedded gitleaks rules: %w", err)
	}
	var vc config.ViperConfig
	if err := v.Unmarshal(&vc); err != nil {
		return nil, fmt.Errorf("decode embedded gitleaks rules: %w", err)
	}
	cfg, err := vc.Translate()
	if err != nil {
		return nil, fmt.Errorf("compile embedded gitleaks rules: %w", err)
	}
	denied := make([]*regexp.Regexp, 0, len(configured.InternalHostnames)+len(configured.DenyTerms))
	for _, term := range append(append([]string(nil), configured.InternalHostnames...), configured.DenyTerms...) {
		if strings.TrimSpace(term) == "" || len(term) > 256 {
			return nil, fmt.Errorf("pack redaction term is invalid")
		}
		pattern, err := regexp.Compile(`(?i)` + regexp.QuoteMeta(term))
		if err != nil {
			return nil, fmt.Errorf("compile pack redaction term: %w", err)
		}
		denied = append(denied, pattern)
	}
	return &SecretScanner{detector: detect.NewDetector(cfg), denied: denied}, nil
}

// Scan redacts Gitleaks matches and PII without returning the matched values in metadata.
func (s *SecretScanner) Scan(input string) (string, []Redaction, error) {
	if s == nil || s.detector == nil {
		return "", nil, fmt.Errorf("secret scanner is not initialized")
	}
	result := input
	hits := make([]Redaction, 0)
	for _, finding := range s.detector.DetectString(result) {
		match := finding.Secret
		if match == "" {
			match = finding.Match
		}
		if match == "" {
			continue
		}
		result = strings.ReplaceAll(result, match, "[REDACTED:gitleaks:"+finding.RuleID+"]")
		hits = append(hits, Redaction{Kind: "gitleaks:" + finding.RuleID, Count: 1})
	}
	clean, pii := Redact(result)
	result = clean
	for _, pattern := range s.denied {
		count := len(pattern.FindAllStringIndex(result, -1))
		if count > 0 {
			result = pattern.ReplaceAllString(result, "[REDACTED:organization]")
			hits = append(hits, Redaction{Kind: "organization", Count: count})
		}
	}
	return result, append(hits, pii...), nil
}
