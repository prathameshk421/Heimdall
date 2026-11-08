package main

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
)

// runCardsConfirm gives one card force after its owner reads it and types yes. A confirmed card can block a merge in
// code its owner may approve, until its text changes.
func runCardsConfirm(args []string, env cardsEnv) int {
	name, rest, ok := confirmArgs("confirm", args, env)
	if !ok {
		return 2
	}
	assumeYes := slices.Contains(rest, "--yes")
	rest = slices.DeleteFunc(slices.Clone(rest), func(a string) bool { return a == "--yes" })
	client, _, ok := remoteSetup("confirm", rest, env)
	if !ok {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), cardsRemoteTimeout)
	defer cancel()
	text, err := client.get(ctx, name)
	if err != nil {
		writeLine(env.out, "heimdall cards confirm: "+err.Error())
		return 1
	}
	if !assumeYes && !confirmedByOwner(text, env.stdin, env.out) {
		writeLine(env.out, "not confirmed")
		return 1
	}
	if err := client.confirmation(ctx, http.MethodPost, name); err != nil {
		writeLine(env.out, "heimdall cards confirm: "+err.Error())
		return 1
	}
	writeLine(env.out, name+" is confirmed. It can block merges in code you approve. Any edit ends this.")
	return 0
}

func runCardsUnconfirm(args []string, env cardsEnv) int {
	name, rest, ok := confirmArgs("unconfirm", args, env)
	if !ok {
		return 2
	}
	client, _, ok := remoteSetup("unconfirm", rest, env)
	if !ok {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), cardsRemoteTimeout)
	defer cancel()
	if err := client.confirmation(ctx, http.MethodDelete, name); err != nil {
		writeLine(env.out, "heimdall cards unconfirm: "+err.Error())
		return 1
	}
	writeLine(env.out, name+" can no longer block a merge")
	return 0
}

func confirmArgs(verb string, args []string, env cardsEnv) (string, []string, bool) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		writeLine(env.out, "usage: heimdall cards "+verb+" NAME --server URL --token-file F")
		return "", nil, false
	}
	return args[0], args[1:], true
}

func confirmedByOwner(text string, in io.Reader, out io.Writer) bool {
	writeLine(out, text)
	writeLine(out, "A finding that cites this card can block a merge in code you approve. Type yes to confirm:")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	return strings.TrimSpace(line) == "yes"
}
