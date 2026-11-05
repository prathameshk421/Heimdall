package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SignatureHeader carries the signature of a delivery: "t=<unix seconds>,v1=<hex HMAC-SHA256>".
const SignatureHeader = "Webhook-Signature"

var (
	// ErrMalformedSignature means the header lacks a usable t= or v1= part.
	ErrMalformedSignature = errors.New("malformed webhook signature")
	// ErrStaleSignature means the timestamp is outside the tolerance, which stops replays of old deliveries.
	ErrStaleSignature = errors.New("webhook signature timestamp is outside the tolerance")
	// ErrBadSignature means the HMAC does not match the body and the secret.
	ErrBadSignature = errors.New("webhook signature does not match")
)

// Sign returns the header value for body delivered at ts, signed with secret.
func Sign(secret []byte, ts time.Time, body []byte) string {
	return fmt.Sprintf("t=%d,v1=%s", ts.Unix(), hex.EncodeToString(mac(secret, ts.Unix(), body)))
}

// Verify checks header against body and secret. A timestamp more than tolerance away from now is refused.
func Verify(secret []byte, header string, body []byte, now time.Time, tolerance time.Duration) error {
	ts, sig, err := parseHeader(header)
	if err != nil {
		return err
	}
	age := now.Sub(time.Unix(ts, 0))
	if age > tolerance || -age > tolerance {
		return ErrStaleSignature
	}
	if !hmac.Equal(sig, mac(secret, ts, body)) {
		return ErrBadSignature
	}
	return nil
}

// mac computes HMAC-SHA256 over "<unix seconds>.<body>", so the timestamp is covered by the signature.
func mac(secret []byte, ts int64, body []byte) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(strconv.FormatInt(ts, 10) + "."))
	h.Write(body)
	return h.Sum(nil)
}

// parseHeader splits "t=..,v1=.." into the timestamp and the decoded signature.
func parseHeader(header string) (int64, []byte, error) {
	var ts int64
	var sig []byte
	var haveTS, haveSig bool
	for _, part := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return 0, nil, fmt.Errorf("%w: %q has no '='", ErrMalformedSignature, part)
		}
		switch key {
		case "t":
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return 0, nil, fmt.Errorf("%w: timestamp: %w", ErrMalformedSignature, err)
			}
			ts, haveTS = n, true
		case "v1":
			b, err := hex.DecodeString(value)
			if err != nil {
				return 0, nil, fmt.Errorf("%w: signature: %w", ErrMalformedSignature, err)
			}
			sig, haveSig = b, true
		}
	}
	if !haveTS || !haveSig {
		return 0, nil, fmt.Errorf("%w: want t= and v1=", ErrMalformedSignature)
	}
	return ts, sig, nil
}
