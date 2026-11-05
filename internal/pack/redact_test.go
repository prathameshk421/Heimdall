package pack

import (
	"strings"
	"testing"
)

func TestRedactRemovesCredentialInBothPasses(t *testing.T) {
	planted := "credential=not-a-real-secret"
	first, err := Redact("notes\n" + planted + "\n")
	if err != nil {
		t.Fatalf("first redaction: %v", err)
	}
	if strings.Contains(first, planted) {
		t.Fatalf("credential survived first output: %q", first)
	}
	second, err := Redact(first)
	if err != nil {
		t.Fatalf("second redaction: %v", err)
	}
	if strings.Contains(second, planted) {
		t.Fatalf("credential survived second output: %q", second)
	}
}

func TestRedactReplacesRecognizedBearerSecret(t *testing.T) {
	got, err := Redact("Authorization: Bearer recognized-token-value\n")
	if err != nil {
		t.Fatalf("recognized bearer secret should be redacted: %v", err)
	}
	if !strings.Contains(got, "Authorization: Bearer [REDACTED:CREDENTIAL]") {
		t.Fatalf("recognized bearer secret was not replaced with a typed placeholder: %q", got)
	}
}

func TestRedactBlocksUnresolvedHit(t *testing.T) {
	_, err := Redact("Authorization: Bearer\n")
	if err == nil {
		t.Fatal("expected unresolved credential-shaped hit to block packing")
	}
}

func TestRedactHighEntropyTokenAndLowEntropyControls(t *testing.T) {
	secret := "q7Vx9Lm2Rp5Nc8Kd"
	got, err := Redact("credential candidate: " + secret)
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if strings.Contains(got, secret) || !strings.Contains(got, "[REDACTED:SECRET]") {
		t.Fatalf("high-entropy candidate not typed-redacted: %q", got)
	}
	for _, input := range []string{"the quick brown fox jumps over the lazy dog", "aaaaaaaaaaaaaaaa", "[REDACTED:SECRET]"} {
		got, err := Redact(input)
		if err != nil || !strings.Contains(got, input) {
			t.Fatalf("low-entropy/control input %q changed or blocked: got %q err %v", input, got, err)
		}
	}
}

func TestRedactDetectorShapes(t *testing.T) {
	cases := []string{
		"AKIAIOSFODNN7EXAMPLE", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.signature",
		"-----BEGIN PRIVATE KEY-----\\nabc\\n-----END PRIVATE KEY-----",
		"postgres://alice:secret@db.example.com/app", "AWS_SECRET_ACCESS_KEY=example-secret",
		"email: alice@example.com", "phone: +1 415 555 2671",
		"card: 4111 1111 1111 1111", "Authorization: Bearer abcdefghijklmnop",
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			got, err := Redact(input)
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			if got == input {
				t.Fatalf("detector did not redact planted shape %q", input)
			}
		})
	}
}

func FuzzRedactNeverReturnsUnresolvedCredential(f *testing.F) {
	f.Add("token=secret-value")
	f.Add("Authorization: Bearer unresolved-token-value")
	f.Fuzz(func(t *testing.T, input string) {
		got, err := Redact(input)
		if err == nil && credentialHit.MatchString(got) && !redactedMarker.MatchString(got) {
			t.Fatalf("unresolved credential returned: %q", got)
		}
	})
}
