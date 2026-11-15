package signer

import (
	"crypto/hmac"
	"crypto/sha1" // BUG G2: card requires sha256
	"encoding/hex"
)

func SignPayload(secret, payload []byte) string {
	mac := hmac.New(sha1.New, secret)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
