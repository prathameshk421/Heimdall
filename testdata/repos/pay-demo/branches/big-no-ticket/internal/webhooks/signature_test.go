package webhooks

import (
	"errors"
	"testing"
	"time"
)

var (
	secret = []byte("whsec_test_0123456789abcdef")
	t0     = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	body   = []byte(`{"id":"evt_1"}`)
)

func TestVerifyAcceptsItsOwnSignature(t *testing.T) {
	header := Sign(secret, t0, body)
	if err := Verify(secret, header, body, t0.Add(time.Second), 5*time.Minute); err != nil {
		t.Fatalf("Verify(own signature) = %v", err)
	}
}

func TestVerifyRefusesAChangedBody(t *testing.T) {
	header := Sign(secret, t0, body)
	err := Verify(secret, header, []byte(`{"id":"evt_2"}`), t0, 5*time.Minute)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify(changed body) = %v, want ErrBadSignature", err)
	}
}

func TestVerifyRefusesAnotherSecret(t *testing.T) {
	header := Sign(secret, t0, body)
	err := Verify([]byte("whsec_other_0123456789abcdef"), header, body, t0, 5*time.Minute)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify(other secret) = %v, want ErrBadSignature", err)
	}
}

func TestVerifyRefusesOldDeliveries(t *testing.T) {
	header := Sign(secret, t0, body)
	err := Verify(secret, header, body, t0.Add(10*time.Minute), 5*time.Minute)
	if !errors.Is(err, ErrStaleSignature) {
		t.Fatalf("Verify(10 minutes old) = %v, want ErrStaleSignature", err)
	}
}

func TestVerifyRefusesMalformedHeaders(t *testing.T) {
	for _, header := range []string{"", "t=1", "v1=00", "garbage", "t=x,v1=00", "t=1,v1=zz"} {
		err := Verify(secret, header, body, t0, 5*time.Minute)
		if !errors.Is(err, ErrMalformedSignature) {
			t.Errorf("Verify(%q) = %v, want ErrMalformedSignature", header, err)
		}
	}
}
