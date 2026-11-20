package policy

import "errors"

// CurrentVersion is the shape of heimdall.yaml this package models: version 2, the seats shape (people.<p>.seat and
// the seats map). The adapter converts version 1 (people.<p>.agent) to it; no other version is read.
const CurrentVersion = 2

// ErrInvalid marks a policy that cannot be used: it does not decode, names a version no reader knows, or fails
// Validate. Callers branch on it with errors.Is to tell a bad file (a need on the change) from an I/O failure (retry).
var ErrInvalid = errors.New("the policy cannot be used")
