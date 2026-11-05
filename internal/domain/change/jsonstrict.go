package change

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"unicode/utf8"
)

var (
	reJSONKey = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	reJSONInt = regexp.MustCompile(`^(0|-?[1-9][0-9]*)$`)
)

// checkStrictJSON refuses the JSON that the canonical form cannot represent faithfully (TECH_SPEC 4.2): invalid
// UTF-8, duplicate keys (which a decoder would silently merge), keys outside [A-Za-z0-9_], fractions and exponents
// (their text has more than one spelling), and anything after the one object. The same check guards decoding and
// hashing, so a payload that would hash ambiguously is never accepted.
func checkStrictJSON(raw []byte) error {
	if !utf8.Valid(raw) {
		return errors.New("is not valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("is not JSON: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errors.New("must be a JSON object")
	}
	if err := walkObject(dec); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("has data after the object")
	}
	return nil
}

// walkObject checks the members of an object whose opening brace was already read.
func walkObject(dec *json.Decoder) error {
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("is not JSON: %w", err)
		}
		key, ok := tok.(string)
		if !ok || !reJSONKey.MatchString(key) {
			return fmt.Errorf("key %v must match [A-Za-z0-9_]+", tok)
		}
		if seen[key] {
			return fmt.Errorf("key %q appears twice", key)
		}
		seen[key] = true
		if err := walkValue(dec); err != nil {
			return err
		}
	}
	_, err := dec.Token() // the closing brace
	return err
}

// walkValue checks one value of any kind.
func walkValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("is not JSON: %w", err)
	}
	switch v := tok.(type) {
	case json.Delim:
		return walkContainer(dec, v)
	case json.Number:
		if !reJSONInt.MatchString(v.String()) {
			return fmt.Errorf("number %s must be an integer written without a leading zero", v)
		}
	}
	return nil
}

// walkContainer checks an array or object that starts with the delimiter open.
func walkContainer(dec *json.Decoder, open json.Delim) error {
	if open == '{' {
		return walkObject(dec)
	}
	for dec.More() {
		if err := walkValue(dec); err != nil {
			return err
		}
	}
	_, err := dec.Token() // the closing bracket
	return err
}
