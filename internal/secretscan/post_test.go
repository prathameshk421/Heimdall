package secretscan

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// H11: a planted token, a canary and a private card's text are each caught; an ordinary post is clean.
func TestScanPostCatchesEachPlantedLeak(t *testing.T) {
	scanner, err := NewSecretScanner()
	require.NoError(t, err)
	card := "The bank drops batch lines above 500 entries silently, so every export must split at 499 lines."
	private := Private{Canaries: []string{"HEIMDALL-CANARY-7f3a91"}, CardTexts: []string{card}}

	token := "ghp_" + strings.Repeat("Zx9", 12)
	tests := []struct {
		name string
		post string
		want []Leak
	}{
		{"ordinary progress", "KAN-22: reading the handler and the spec now.", nil},
		{"a planted token", "here is the key I used: " + token, []Leak{LeakCredential}},
		{"a canary", "context says HEIMDALL-CANARY-7f3a91 for this", []Leak{LeakCanary}},
		{"card text, reflowed", "note: the bank drops batch lines   above 500 entries silently, so every export must split", []Leak{LeakCardText}},
		{"a few words in common are fine", "the export must split", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ScanPost(tc.post, scanner, private)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
