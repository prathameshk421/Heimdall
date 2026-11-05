package main

import (
	"os"
	"strings"
	"testing"
)

func TestMakefileRunsBudgetGateBeforeLiveE2E(t *testing.T) {
	data, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	check := strings.Index(text, "budget-check")
	run := strings.Index(text, "budget-run")
	if strings.Contains(text, "TODO(T19)") || check < 0 || run < 0 || check > run || !strings.Contains(text, "run-tagged.sh e2e") {
		t.Fatalf("Makefile e2e has no ordered live budget check and recording wrapper")
	}
}
