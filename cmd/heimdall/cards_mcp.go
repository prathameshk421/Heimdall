package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"path/filepath"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/cardsfs"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/cardsmcp"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/dsse"
)

type localCardSource struct {
	dir string
}

func (s localCardSource) Cards(_ context.Context) ([]cardsmcp.CardEntry, error) {
	names, err := cardFileNames(s.dir)
	if err != nil {
		return nil, err
	}
	entries := make([]cardsmcp.CardEntry, 0, len(names))
	for _, name := range names {
		raw, err := readInDir(s.dir, name)
		if err != nil {
			continue
		}
		card, err := cardsfs.Parse(name, string(raw))
		if err != nil {
			continue
		}
		entries = append(entries, cardsmcp.CardEntry{
			Card:   card,
			Signed: hasMatchingEnvelope(s.dir, name, raw),
		})
	}
	return entries, nil
}

func hasMatchingEnvelope(dir, name string, cardBytes []byte) bool {
	envRaw, err := readInDir(dir, name+".dsse.json")
	if err != nil {
		return false
	}
	var env dsse.Envelope
	if err := json.Unmarshal(envRaw, &env); err != nil {
		return false
	}
	if env.PayloadType != dsse.PayloadTypeRuleCard || len(env.Signatures) == 0 {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(env.Payload)
	return err == nil && bytes.Equal(decoded, cardBytes)
}

// runCardsMCP starts the read-only local stdio MCP server for approved personal rule cards (B4).
func runCardsMCP(args []string, env cardsEnv) int {
	fs := flag.NewFlagSet("heimdall cards mcp", flag.ContinueOnError)
	fs.SetOutput(env.errw)
	dir := fs.String("dir", filepath.Join(defaultCardsWork, "files"), "folder with the card files")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		writeLine(env.errw, "usage: heimdall cards mcp [--dir dir]")
		return 2
	}
	srv, err := cardsmcp.New(localCardSource{dir: *dir})
	if err != nil {
		writeLine(env.errw, "heimdall cards mcp: "+err.Error())
		return 1
	}
	if err := srv.Serve(context.Background(), env.stdin, env.out); err != nil {
		writeLine(env.errw, "heimdall cards mcp: "+err.Error())
		return 1
	}
	return 0
}
