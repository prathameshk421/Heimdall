---
applies_to: ["**/Dockerfile*", "**/compose*.y*ml", "**/*.tf"]
check: "Does the image run as a non-root user, with a read-only root filesystem and no secret in an environment file?"
proof: "Start the container and show the user id it runs as and the paths it can write to."
kind: convention
source: heimdall lens template: infra
status: active
---
A container that runs as root turns any code that runs inside it into root on the host's namespaces, and the root
filesystem that can be written to keeps whatever is stolen.

The image needs a user that is not root, a root filesystem that cannot be written to, and the writable paths named
one by one. A secret in an environment variable is readable by every process in the container and is printed by
every crash dump.
