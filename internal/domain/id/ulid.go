package id

import (
	"encoding/binary"
	"fmt"
	"regexp"
)

// reULID accepts 26 Crockford base32 characters with no I, L, O or U. The first character is limited to 0-7 because
// 26 characters hold 130 bits and the id holds 128: a larger first character would overflow the 48-bit timestamp.
var reULID = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)

const (
	crockford    = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	maxULIDMilli = 1<<48 - 1
)

// ULID is a 26-character id: a 48-bit millisecond timestamp, then 80 random bits, in Crockford base32. The
// generators here take the time and the entropy as arguments, so the domain never reads a clock or a random source.
type ULID string

// ParseULID validates s. Only upper case is accepted, so one id has one text form.
func ParseULID(s string) (ULID, error) {
	if !reULID.MatchString(s) {
		return "", fmt.Errorf("%q is not a 26-character ULID", s)
	}
	return ULID(s), nil
}

// String returns the id as text.
func (u ULID) String() string { return string(u) }

// Valid reports whether the id passes ParseULID.
func (u ULID) Valid() bool {
	_, err := ParseULID(string(u))
	return err == nil
}

// NewULID encodes a millisecond timestamp and 80 bits of entropy. It fails when the timestamp needs more than 48
// bits, rather than truncating it.
func NewULID(unixMillis uint64, entropy [10]byte) (ULID, error) {
	if unixMillis > maxULIDMilli {
		return "", fmt.Errorf("ulid timestamp %d does not fit in 48 bits", unixMillis)
	}
	var b [26]byte
	t := unixMillis
	for i := 9; i >= 0; i-- {
		b[i] = crockford[t&31]
		t >>= 5
	}
	var acc uint64
	var bits uint
	pos := 10
	for _, x := range entropy {
		acc = acc<<8 | uint64(x)
		bits += 8
		for bits >= 5 {
			bits -= 5
			b[pos] = crockford[(acc>>bits)&31]
			pos++
		}
		acc &= 1<<bits - 1
	}
	return ULID(b[:]), nil
}

// Time returns the millisecond timestamp of the id.
func (u ULID) Time() (uint64, error) {
	if !reULID.MatchString(string(u)) {
		return 0, fmt.Errorf("%q is not a 26-character ULID", u)
	}
	var t uint64
	for i := 0; i < 10; i++ {
		t = t<<5 | crockfordValue(u[i])
	}
	return t, nil
}

// entropy returns the 80 random bits of a valid id.
func (u ULID) entropy() [10]byte {
	var out [10]byte
	var acc uint64
	var bits uint
	k := 0
	for i := 10; i < 26; i++ {
		acc = acc<<5 | crockfordValue(u[i])
		bits += 5
		if bits >= 8 {
			bits -= 8
			var w [8]byte
			binary.BigEndian.PutUint64(w[:], acc>>bits)
			out[k] = w[7]
			k++
			acc &= 1<<bits - 1
		}
	}
	return out
}

// crockfordValue is the 5-bit value of a character that reULID already admitted.
func crockfordValue(c byte) uint64 {
	var v uint64
	for i := 0; i < len(crockford); i++ {
		if crockford[i] == c {
			return v
		}
		v++
	}
	return 0
}

// Next returns an id strictly greater than prev. A later timestamp takes the given entropy. Otherwise (the same
// millisecond, or a clock that stepped back) it keeps prev's timestamp and adds one to its random part, so the order
// holds. It fails when that random part is already at its maximum.
func Next(prev ULID, nowMillis uint64, entropy [10]byte) (ULID, error) {
	prevMilli, err := prev.Time()
	if err != nil {
		return "", fmt.Errorf("next ulid: %w", err)
	}
	if nowMillis > prevMilli {
		return NewULID(nowMillis, entropy)
	}
	rnd := prev.entropy()
	for i := len(rnd) - 1; i >= 0; i-- {
		rnd[i]++
		if rnd[i] != 0 {
			return NewULID(prevMilli, rnd)
		}
	}
	return "", fmt.Errorf("next ulid: random part of %s is exhausted within millisecond %d", prev, prevMilli)
}

// UnmarshalText validates while decoding and leaves u unchanged on error.
func (u *ULID) UnmarshalText(b []byte) error {
	v, err := ParseULID(string(b))
	if err != nil {
		return err
	}
	*u = v
	return nil
}
