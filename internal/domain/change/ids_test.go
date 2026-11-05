package change_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

func TestParseEventIDAcceptsOnlyLowercaseUUIDs(t *testing.T) {
	ok, err := change.ParseEventID("3f1c9a52-8d2b-4e7a-9c11-0b6f5d4e3a21")
	require.NoError(t, err)
	require.Equal(t, "3f1c9a52-8d2b-4e7a-9c11-0b6f5d4e3a21", ok.String())
	for _, bad := range []string{
		"", "3F1C9A52-8D2B-4E7A-9C11-0B6F5D4E3A21", // uppercase is not the canonical form the table stores
		"3f1c9a528d2b4e7a9c110b6f5d4e3a21",      // no hyphens
		"3f1c9a52-8d2b-4e7a-9c11-0b6f5d4e3a2",   // one digit short
		"3f1c9a52-8d2b-4e7a-9c11-0b6f5d4e3a211", // one digit long
		"3f1c9a52-8d2b-4e7a-9c11-0b6f5d4e3a2g",  // not hex
		"fnd_01HZX3K9QW7M2P5R8T0V4YB6CD",        // a prefixed ULID is not an event id
	} {
		_, err := change.ParseEventID(bad)
		require.Error(t, err, "should reject %q", bad)
	}
}

func TestEventIDDecodesFromJSONAndRefusesBadValues(t *testing.T) {
	var got change.EventID
	require.NoError(t, json.Unmarshal([]byte(`"3f1c9a52-8d2b-4e7a-9c11-0b6f5d4e3a21"`), &got))
	require.Error(t, json.Unmarshal([]byte(`"not-an-id"`), &got))
}

func TestOpaqueSwitchIdentifiersAreNonEmptyPrintableAndBounded(t *testing.T) {
	for _, parse := range []func(string) error{
		func(s string) error { _, err := change.ParseRoomID(s); return err },
		func(s string) error { _, err := change.ParseSenderID(s); return err },
		func(s string) error { _, err := change.ParseMessageID(s); return err },
		func(s string) error { _, err := change.ParseQuestionID(s); return err },
		func(s string) error { _, err := change.ParseCorrelationID(s); return err },
		func(s string) error { _, err := change.ParseActorID(s); return err },
	} {
		require.NoError(t, parse("ok-id_1.2:3"))
		require.Error(t, parse(""))
		require.Error(t, parse("has space"))
		require.Error(t, parse("line\nbreak"))
		require.Error(t, parse(strings.Repeat("a", 129)))
		require.NoError(t, parse(strings.Repeat("a", 128)))
	}
}

func TestOpaqueIDUnmarshalValidates(t *testing.T) {
	var r change.RoomID
	require.NoError(t, json.Unmarshal([]byte(`"room-42"`), &r))
	require.Equal(t, "room-42", r.String())
	require.Error(t, json.Unmarshal([]byte(`""`), &r))
}

func TestParseRepo(t *testing.T) {
	r, err := change.ParseRepo("acme/pay-demo")
	require.NoError(t, err)
	require.Equal(t, "acme/pay-demo", r.String())
	for _, bad := range []string{"", "acme", "acme/", "/pay", "acme/pay/demo", "acme pay/demo"} {
		_, err := change.ParseRepo(bad)
		require.Error(t, err, "should reject %q", bad)
	}
}
