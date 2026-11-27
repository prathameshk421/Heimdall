// Package action carries the GitHub Action mode workflows inside the heimdall binary, so `heimdall action init -dir`
// can write them into a repository without a checkout of Heimdall.
package action

import "embed"

// Workflows holds the trigger, proof and gate workflows. Each `uses:` of this action ends with `# heimdall-pin` and
// pins the all-zero SHA, which init replaces with the Heimdall commit being installed.
//
//go:embed workflows/heimdall-trigger.yml workflows/heimdall-proof.yml workflows/heimdall-gate.yml
var Workflows embed.FS
