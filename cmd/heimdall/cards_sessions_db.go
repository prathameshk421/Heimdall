package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/sessions"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/sessions/cursor"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/sessions/opencode"
)

// dbSessions reads the agents that keep sessions in SQLite: OpenCode and Cursor. Both readers open the files
// read-only and never change them. A source that cannot be read is named on stderr and skipped, so one bad
// database does not stop the scan.
func dbSessions(ctx context.Context, env cardsEnv, root string, cutoff time.Time) []sessions.Session {
	lim := sessions.Limits{Since: cutoff, MaxSessions: maxSessionFiles}
	var out []sessions.Session
	if db := opencode.DefaultPath(env.home); isRegular(db) {
		got, err := opencode.Read(ctx, db, root, lim)
		if err != nil {
			writeLine(env.errw, "opencode sessions: "+err.Error())
		}
		out = append(out, got...)
	}
	for _, dir := range existingCursorDirs(env.home) {
		got, err := cursor.Read(ctx, dir, root, lim)
		if err != nil {
			writeLine(env.errw, "cursor sessions: "+err.Error())
		}
		out = append(out, got...)
	}
	return out
}

// existingCursorDirs lists the Cursor "User" folders that hold a global chat store.
func existingCursorDirs(home string) []string {
	var dirs []string
	for _, d := range cursor.UserDirs(home) {
		if isRegular(filepath.Join(d, "globalStorage", "state.vscdb")) {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

func isRegular(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
