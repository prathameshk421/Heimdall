// lintselftest: path=internal/domain/fxdepguarddomain/x.go
// lintselftest: want=depguard|'domain'

// Package fxdepguarddomain is a lint fixture.
package fxdepguarddomain

import _ "github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
