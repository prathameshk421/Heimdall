// lintselftest: path=internal/platform/fxdepguardtestkit/x.go
// lintselftest: want=depguard|'production-has-no-testkit'

// Package fxdepguardtestkit is a lint fixture.
package fxdepguardtestkit

import _ "github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit"
