package signer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

func SignPayload(secret, payload []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
