---
applies_to: []
check: "Is every secret read from the environment or a secret store, never from the source tree, a default or a command line?"
proof: "Show the line that reads the secret and the place the value comes from at run time."
kind: convention
source: heimdall lens template: security
status: active
---
A secret in the source tree is in every clone, in the history, and in the build image. A secret as a default value is
a secret that keeps working when the environment forgets to set it, and nobody notices until the environment is
wrong. A secret on a command line is in the process list of every user on the machine.

The value must come from the environment or from a secret store at run time, and the code must fail when it is
missing rather than carry a working default. A secret that a test needs belongs in the test's own fake, never in
the product code.
