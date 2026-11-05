package policy

import (
	"encoding/json"
	"fmt"
	"time"
)

// Duration is a length of time that the policy file writes as a string such as 15m or 4h. It is a
// distinct type so a bare number, which has no unit, cannot be read as a duration by accident.
type Duration time.Duration

// parseDuration reads a policy duration. Negative values are refused here, because no timeout or
// interval in the policy can mean a moment before it started. Zero is allowed by the decoder and refused
// by Validate where a zero value would disable the timeout.
func parseDuration(s string) (Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("duration %q: use a number with a unit such as 15m or 4h: %w", s, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("duration %q must not be negative", s)
	}
	return Duration(d), nil
}

// String writes the duration in its shortest exact form: 4h, 90m, 500ms. The author's spelling survives a
// round trip, which time.Duration.String does not (it writes 4h0m0s).
func (d Duration) String() string {
	t := time.Duration(d)
	switch {
	case t == 0:
		return "0s"
	case t%time.Hour == 0:
		return fmt.Sprintf("%dh", int64(t/time.Hour))
	case t%time.Minute == 0:
		return fmt.Sprintf("%dm", int64(t/time.Minute))
	case t%time.Second == 0:
		return fmt.Sprintf("%ds", int64(t/time.Second))
	case t%time.Millisecond == 0:
		return fmt.Sprintf("%dms", int64(t/time.Millisecond))
	}
	return t.String()
}

// UnmarshalJSON reads a duration string. A bare number is an error: it has no unit.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string such as \"15m\": %w", err)
	}
	v, err := parseDuration(s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// MarshalJSON writes the duration as the string the author would write.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

// UnmarshalYAML reads a duration written as a YAML scalar. It uses the yaml.v3 callback form so the domain
// package does not import the YAML library.
func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return fmt.Errorf("duration must be a scalar such as 15m: %w", err)
	}
	v, err := parseDuration(s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// MarshalYAML writes the duration as the string the author would write.
func (d Duration) MarshalYAML() (any, error) { return d.String(), nil }
