package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLinkCodeReadsTheCodeOfALinkCommand(t *testing.T) {
	cases := []struct {
		text string
		src  Source
		code string
		ok   bool
	}{
		{"/heimdall link K7Q2XM", SourceGitHub, "K7Q2XM", true},
		{"/heimdall LINK k7q2xm", SourceGitHub, "K7Q2XM", true},
		{"  /heimdall link   K7Q2XM  ", SourceGitHub, "K7Q2XM", true},
		{"heimdall link K7Q2XM", SourceChat, "K7Q2XM", true},
		{"/heimdall link", SourceGitHub, "", false},
		{"/heimdall link K7Q2XM extra", SourceGitHub, "", false},
		{"/heimdall link K7Q2X", SourceGitHub, "", false},
		{"/heimdall link K7Q2X!", SourceGitHub, "", false},
		{"/heimdall uphold F1", SourceGitHub, "", false},
		{"heimdall link K7Q2XM", SourceGitHub, "", false},
	}
	for _, c := range cases {
		code, ok := LinkCode(Input{Text: c.text, Source: c.src})
		require.Equal(t, c.ok, ok, c.text)
		require.Equal(t, c.code, code, c.text)
	}
}
