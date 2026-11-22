package redact

import (
	"strings"
	"testing"
)

func TestScrubSecrets(t *testing.T) {
	tests := []struct {
		name, value, rule string
	}{
		{"github classic", "ghp_" + strings.Repeat("a", 30), "github_token"},
		{"github oauth", "gho_" + strings.Repeat("b", 30), "github_token"},
		{"github user", "ghu_" + strings.Repeat("c", 30), "github_token"},
		{"github server", "ghs_" + strings.Repeat("d", 30), "github_token"},
		{"github refresh", "ghr_" + strings.Repeat("e", 30), "github_token"},
		{"github fine grained", "github_pat_" + strings.Repeat("f", 30), "github_token"},
		{"aws access", "AKIA" + strings.Repeat("A", 16), "aws_access_key"},
		{"aws session", "ASIA" + strings.Repeat("B", 16), "aws_access_key"},
		{"openai", "sk-" + strings.Repeat("cD8", 8), "openai_anthropic_openrouter_key"},
		{"anthropic", "sk-ant-" + strings.Repeat("dE9", 8), "openai_anthropic_openrouter_key"},
		{"openrouter", "sk-or-v1-" + strings.Repeat("eF0", 8), "openai_anthropic_openrouter_key"},
		{"stripe secret", "sk_live_" + strings.Repeat("g", 20), "stripe_key"},
		{"stripe restricted", "rk_live_" + strings.Repeat("h", 20), "stripe_key"},
		{"slack bot", "xoxb-" + strings.Repeat("i", 20), "slack_token"},
		{"slack user", "xoxp-" + strings.Repeat("j", 20), "slack_token"},
		{"slack app", "xoxa-" + strings.Repeat("k", 20), "slack_token"},
		{"jwt", "eyJ" + "hbGciOiJIUzI1NiJ9." + "eyJ" + "zdWIiOiIxMjM0NTY3ODkwIn0.signature-0123456789", "jwt"},
		{"authorization bearer", "Bearer " + strings.Repeat("z", 24), "authorization"},
		{"authorization basic", "Basic " + strings.Repeat("Y", 24), "authorization"},
		{"url credentials", "https://alice:secret-pass@example.test/path", "url_credentials"},
		{"credential assignment", "api_key=" + "A7x9Q2b5" + "M8n1P4r6" + "C3v0", "credential_assignment"},
		{"private key", "-----BEGIN RSA " + "PRIVATE KEY-----\nfake-key-material\n-----END RSA " + "PRIVATE KEY-----", "private_key"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clean, report := Scrub("value=" + test.value)
			if strings.Contains(clean, test.value) {
				t.Fatalf("secret remained in output: %q", clean)
			}
			if !report.HadSecrets || report.HadPII || report.TotalReplacements != 1 {
				t.Fatalf("unexpected report: %+v", report)
			}
			if len(report.Counts) != 1 || report.Counts[0].Rule != test.rule {
				t.Fatalf("unexpected rule counts: %+v", report.Counts)
			}
			if strings.Contains(FormatReport(report), test.value) {
				t.Fatal("report exposed matched value")
			}
		})
	}
}

func TestScrubQuotedCredentialAssignment(t *testing.T) {
	clean, report := Scrub(`{"password": "` + "A7x9Q2b5" + "M8n1P4r6" + `C3v0"}`)
	if !strings.Contains(clean, "[REDACTED:secret:credential_assignment]") || report.TotalReplacements != 1 {
		t.Fatalf("quoted credential was not scrubbed: %q %+v", clean, report)
	}
}

func TestScrubPII(t *testing.T) {
	tests := []struct{ name, input, want string }{
		{"email", "contact dev.person@example.test", "contact [REDACTED:pii:email]"},
		{"phone", "call +1 (415) 555-2671", "call [REDACTED:pii:phone]"},
		{"private ip", "host 10.20.30.40", "host [REDACTED:pii:ip]"},
		{"public ip", "host 8.8.4.4", "host [REDACTED:pii:ip]"},
		{"mac home", "file /Users/alice/project/main.go", "file /Users/[REDACTED:pii:user]/project/main.go"},
		{"linux home", "file /home/alice/project/main.go", "file /home/[REDACTED:pii:user]/project/main.go"},
		{"windows home", `file C:\Users\alice\project\main.go`, `file C:\Users\[REDACTED:pii:user]\project\main.go`},
		{"credit card", "card 4111-1111-1111-1111", "card [REDACTED:pii:credit_card]"},
		{"ssn", "id 123-45-6789", "id [REDACTED:pii:ssn]"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clean, report := Scrub(test.input)
			if clean != test.want {
				t.Fatalf("Scrub() = %q, want %q", clean, test.want)
			}
			if !report.HadPII || report.HadSecrets || report.TotalReplacements != 1 {
				t.Fatalf("unexpected report: %+v", report)
			}
		})
	}
}

func TestScrubKeepsOrdinaryCodeAndNonPIIAddresses(t *testing.T) {
	input := strings.Join([]string{
		`import "fmt"`,
		`import { useState } from "@scope/pkg"`,
		`use std::collections::HashMap;`,
		`const version = "v1.2.3";`,
		`const longVersion = "v1234567890";`,
		`const commit = "0123456789abcdef0123456789abcdef01234567";`,
		`const id = "01ARZ3NDEKTSV4RRFFQ69G5FAV";`,
		`func TestScrubKeepsOrdinaryCodeAndNonPIIAddresses(t *testing.T) {}`, // ordinary test name
		`listen := "127.0.0.1:8080"`,
		`bind := "0.0.0.0"`,
		`doc := "192.0.2.1"`,
		`date := "2025-11-22 12:30:45"`,
	}, "\n")
	clean, report := Scrub(input)
	if clean != input || report.TotalReplacements != 0 {
		t.Fatalf("ordinary code changed:\n%s\nreport: %+v", clean, report)
	}
}

func TestScrubReportAndLinesAreDeterministic(t *testing.T) {
	phone := "+1 (415) 555-2671"
	lines, report := ScrubLines([]string{"email dev@example.test", "call " + phone, "email ops@example.test"})
	if len(lines) != 3 || report.TotalReplacements != 3 || !report.HadPII || report.HadSecrets {
		t.Fatalf("unexpected output/report: %#v %+v", lines, report)
	}
	if len(report.Counts) != 2 || report.Counts[0] != (Count{Kind: "pii", Rule: "email", Count: 2}) || report.Counts[1] != (Count{Kind: "pii", Rule: "phone", Count: 1}) {
		t.Fatalf("counts are not sorted/aggregated: %+v", report.Counts)
	}
	if FormatReport(Report{}) != "No sensitive values found." {
		t.Fatal("empty report summary is not concise")
	}
}

func TestScrubLinesRedactsMultilinePrivateKey(t *testing.T) {
	lines := []string{"private key follows", "-----BEGIN RSA PRIVATE KEY-----", "synthetic-key-material", "-----END RSA PRIVATE KEY-----", "done"}
	clean, report := ScrubLines(lines)
	if len(clean) != len(lines) || clean[1] != "[REDACTED:secret:private_key]" || report.TotalReplacements != 1 || !report.HadSecrets {
		t.Fatalf("multiline key was not scrubbed in place: %#v %+v", clean, report)
	}
}

func TestScrubDoesNotExposeMatchedDataInReport(t *testing.T) {
	input := "mail private@example.test token=" + "A7x9Q2b5" + "M8n1P4r6" + "C3v0"
	_, report := Scrub(input)
	formatted := FormatReport(report)
	if strings.Contains(formatted, "private@example.test") || strings.Contains(formatted, "A7x9Q2b5M8n1P4r6C3v0") {
		t.Fatalf("report included matched data: %q", formatted)
	}
}

func FuzzScrub(f *testing.F) {
	f.Add("ordinary source code")
	f.Add("email user@example.test and ip 10.20.30.40")
	f.Add("token=" + strings.Repeat("x7Q2", 12))
	f.Fuzz(func(t *testing.T, input string) {
		clean, report := Scrub(input)
		if report.TotalReplacements < 0 || len(clean) == 0 && input != "" {
			t.Fatalf("invalid scrub result: length=%d report=%+v", len(clean), report)
		}
		for i, count := range report.Counts {
			if count.Count <= 0 || i > 0 && (report.Counts[i-1].Kind > count.Kind || report.Counts[i-1].Kind == count.Kind && report.Counts[i-1].Rule > count.Rule) {
				t.Fatalf("invalid or unsorted count: %+v", report.Counts)
			}
		}
	})
}
