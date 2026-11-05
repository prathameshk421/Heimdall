// lintselftest: path=internal/platform/fxsloglint/x.go
// lintselftest: want=sloglint|default logger

// Package fxsloglint is a lint fixture.
package fxsloglint

import "log/slog"

// Say uses the global logger.
func Say() { slog.Info("x") }
