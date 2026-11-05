// lintselftest: path=internal/platform/fxcontextcheck/x.go
// lintselftest: want=contextcheck|should pass the context parameter

// Package fxcontextcheck is a lint fixture.
package fxcontextcheck

import "context"

// Outer has a context and drops it.
func Outer(ctx context.Context) { _ = ctx; inner() }

func inner() { sink(context.Background()) }

func sink(ctx context.Context) { _ = ctx }
