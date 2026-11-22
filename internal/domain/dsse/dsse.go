package dsse

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// PayloadTypeRuleCard is the media type used for serialized rule cards.
const PayloadTypeRuleCard = "application/vnd.heimdall.rulecard+json"

// Envelope is a DSSE v1 envelope. Payload and signatures use standard base64.
type Envelope struct {
	PayloadType string      `json:"payloadType"`
	Payload     string      `json:"payload"`
	Signatures  []Signature `json:"signatures"`
}

// Signature is one DSSE signature associated with its trusted key identifier.
type Signature struct {
	KeyID string `json:"keyid"`
	Sig   string `json:"sig"`
}

// Sign signs payload using the DSSE v1 pre-authentication encoding.
func Sign(payloadType string, payload []byte, keyID string, priv ed25519.PrivateKey) (Envelope, error) {
	if strings.TrimSpace(payloadType) == "" {
		return Envelope{}, errors.New("payload type is empty")
	}
	if strings.TrimSpace(keyID) == "" {
		return Envelope{}, errors.New("key id is empty")
	}
	if len(priv) != ed25519.PrivateKeySize {
		return Envelope{}, fmt.Errorf("private key has length %d, want %d", len(priv), ed25519.PrivateKeySize)
	}
	pae := preAuthEncode(payloadType, payload)
	sig := ed25519.Sign(priv, pae)
	return Envelope{
		PayloadType: payloadType,
		Payload:     base64.StdEncoding.EncodeToString(payload),
		Signatures:  []Signature{{KeyID: keyID, Sig: base64.StdEncoding.EncodeToString(sig)}},
	}, nil
}

// Verify checks payload type, encoding and a trusted ed25519 signature, returning the payload and signing key id.
func Verify(env Envelope, expectedType string, trustedKeys map[string]ed25519.PublicKey) ([]byte, string, error) {
	if env.PayloadType != expectedType || expectedType == "" {
		return nil, "", fmt.Errorf("payload type %q does not match expected type %q", env.PayloadType, expectedType)
	}
	payload, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil {
		return nil, "", fmt.Errorf("decode DSSE payload: %w", err)
	}
	if len(env.Signatures) == 0 {
		return nil, "", errors.New("DSSE envelope has no signatures")
	}
	pae := preAuthEncode(env.PayloadType, payload)
	var verifiedKeyID string
	for _, signature := range env.Signatures {
		key, ok := trustedKeys[signature.KeyID]
		if !ok {
			return nil, "", fmt.Errorf("DSSE key id %q is not trusted", signature.KeyID)
		}
		if len(key) != ed25519.PublicKeySize {
			return nil, "", fmt.Errorf("trusted key %q has invalid length", signature.KeyID)
		}
		sig, err := base64.StdEncoding.DecodeString(signature.Sig)
		if err != nil {
			return nil, "", fmt.Errorf("decode DSSE signature for %q: %w", signature.KeyID, err)
		}
		if !ed25519.Verify(key, pae, sig) {
			return nil, "", fmt.Errorf("DSSE signature verification failed for %q", signature.KeyID)
		}
		if verifiedKeyID == "" {
			verifiedKeyID = signature.KeyID
		}
	}
	return payload, verifiedKeyID, nil
}

func preAuthEncode(payloadType string, payload []byte) []byte {
	var b strings.Builder
	b.Grow(len(payloadType) + len(payload) + 64)
	b.WriteString("DSSEv1 ")
	b.WriteString(strconv.Itoa(len(payloadType)))
	b.WriteByte(' ')
	b.WriteString(payloadType)
	b.WriteByte(' ')
	b.WriteString(strconv.Itoa(len(payload)))
	b.WriteByte(' ')
	b.Write(payload)
	return []byte(b.String())
}

// VerifyRuleCardScope fails closed unless every card glob is provably contained in an owner-approved glob.
func VerifyRuleCardScope(cardPaths, ownerApprovedGlobs []string) error {
	if len(cardPaths) == 0 || len(ownerApprovedGlobs) == 0 {
		return errors.New("card scope and owner-approved scope must be non-empty")
	}
	for _, approved := range ownerApprovedGlobs {
		if err := policy.ValidateGlob(approved); err != nil {
			return fmt.Errorf("invalid owner-approved glob %q: %w", approved, err)
		}
	}
	for _, cardPath := range cardPaths {
		if err := policy.ValidateGlob(cardPath); err != nil {
			return fmt.Errorf("invalid card glob %q: %w", cardPath, err)
		}
		contained := false
		for _, approved := range ownerApprovedGlobs {
			if globWithin(cardPath, approved) {
				contained = true
				break
			}
		}
		if !contained {
			return fmt.Errorf("card glob %q is outside the owner's approved scope", cardPath)
		}
	}
	return nil
}

func globWithin(cardPath, approved string) bool {
	if cardPath == approved || approved == "**" {
		return true
	}
	if !strings.ContainsAny(cardPath, "*?[") {
		return policy.MatchGlob(approved, cardPath)
	}
	if !strings.HasSuffix(approved, "/**") {
		return false
	}
	prefix := strings.TrimSuffix(approved, "/**")
	if strings.ContainsAny(prefix, "*?[") {
		return false
	}
	return strings.HasPrefix(cardPath, prefix+"/")
}
