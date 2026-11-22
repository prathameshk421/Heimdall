package dsse

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func testKeys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv := testKeys(t)
	payload := []byte(`{"body":"keep refunds safe"}`)
	env, err := Sign(PayloadTypeRuleCard, payload, "alice", priv)
	if err != nil {
		t.Fatal(err)
	}
	got, keyID, err := Verify(env, PayloadTypeRuleCard, map[string]ed25519.PublicKey{"alice": pub})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) || keyID != "alice" {
		t.Fatalf("Verify = %q, %q", got, keyID)
	}
}

func TestVerifyRejectsTamperingAndUntrustedSigners(t *testing.T) {
	pub, priv := testKeys(t)
	env, err := Sign(PayloadTypeRuleCard, []byte("card"), "alice", priv)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]ed25519.PublicKey{"alice": pub}
	cases := map[string]func(*Envelope){
		"payload":      func(e *Envelope) { e.Payload = base64.StdEncoding.EncodeToString([]byte("changed")) },
		"payload type": func(e *Envelope) { e.PayloadType = "other/type" },
		"signature": func(e *Envelope) {
			e.Signatures[0].Sig = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
		},
		"unknown key":      func(e *Envelope) { e.Signatures[0].KeyID = "unknown" },
		"empty signatures": func(e *Envelope) { e.Signatures = nil },
		"bad base64":       func(e *Envelope) { e.Payload = "%%%" },
	}
	for name, tamper := range cases {
		t.Run(name, func(t *testing.T) {
			bad := env
			bad.Signatures = append([]Signature(nil), env.Signatures...)
			tamper(&bad)
			if _, _, err := Verify(bad, PayloadTypeRuleCard, keys); err == nil {
				t.Fatal("Verify accepted tampered envelope")
			}
		})
	}
	wrongPub, _ := testKeys(t)
	if _, _, err := Verify(env, PayloadTypeRuleCard, map[string]ed25519.PublicKey{"alice": wrongPub}); err == nil {
		t.Fatal("wrong key accepted")
	}
}

func TestVerifyRuleCardScope(t *testing.T) {
	for name, tc := range map[string]struct {
		paths, scope []string
		wantErr      bool
	}{
		"same":           {[]string{"internal/pay/**"}, []string{"internal/pay/**"}, false},
		"narrower":       {[]string{"internal/pay/refunds/**"}, []string{"internal/pay/**"}, false},
		"literal within": {[]string{"internal/pay/refund.go"}, []string{"internal/pay/**"}, false},
		"outside":        {[]string{"internal/auth/**"}, []string{"internal/pay/**"}, true},
		"traversal":      {[]string{"internal/../secret"}, []string{"**"}, true},
		"ambiguous":      {[]string{"internal/*/secret.go"}, []string{"internal/pay/**"}, true},
	} {
		t.Run(name, func(t *testing.T) {
			err := VerifyRuleCardScope(tc.paths, tc.scope)
			if (err != nil) != tc.wantErr {
				t.Fatalf("VerifyRuleCardScope error = %v", err)
			}
		})
	}
}

func FuzzVerify(f *testing.F) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		f.Fatal(err)
	}
	env, err := Sign(PayloadTypeRuleCard, []byte("card"), "test", priv)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(env.Payload, env.PayloadType, env.Signatures[0].Sig)
	f.Fuzz(func(t *testing.T, payload, payloadType, sig string) {
		t.Helper()
		candidate := Envelope{Payload: payload, PayloadType: payloadType, Signatures: []Signature{{KeyID: "test", Sig: sig}}}
		_, _, _ = Verify(candidate, PayloadTypeRuleCard, map[string]ed25519.PublicKey{"test": pub})
	})
}
