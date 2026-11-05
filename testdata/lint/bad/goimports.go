// lintselftest: path=internal/platform/fxgoimports/x.go
// lintselftest: want=goimports|not properly formatted

// Package fxgoimports is a lint fixture.
package fxgoimports

import (
	"fmt"
	"github.com/google/go-cmp/cmp"
	"os"
)

// F uses its imports.
func F() { _ = fmt.Sprint(os.Args, cmp.Equal(1, 1)) }
