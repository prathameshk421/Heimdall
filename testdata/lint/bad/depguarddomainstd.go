// lintselftest: path=internal/domain/fxdomainstd/x.go
// lintselftest: want=depguard|'os' is not allowed from list 'domain'
// lintselftest: want=depguard|'net/http' is not allowed from list 'domain'
// lintselftest: want=depguard|'os/exec' is not allowed from list 'domain'
// lintselftest: want=depguard|'math/rand' is not allowed from list 'domain'
// lintselftest: want=depguard|'crypto/rand' is not allowed from list 'domain'
// lintselftest: want=depguard|'log' is not allowed from list 'domain'
// lintselftest: want=depguard|'io/fs' is not allowed from list 'domain'
// lintselftest: want=depguard|'syscall' is not allowed from list 'domain'
// lintselftest: want=depguard|'path/filepath'

// Package fxdomainstd checks R5: the domain imports no package that does I/O or draws randomness.
package fxdomainstd

import (
	_ "crypto/rand"
	_ "io/fs"
	_ "log"
	_ "math/rand"
	_ "net/http"
	_ "os"
	_ "os/exec"
	_ "path/filepath"
	_ "syscall"
)
