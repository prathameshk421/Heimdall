---
applies_to: ["**/Dockerfile*", "**/*.tf", "**/.github/workflows/**", "**/compose*.y*ml"]
check: "Is every image and every action reference pinned to an exact version or digest, and not to a moving tag?"
proof: "Show the reference and the digest it resolves to today."
kind: convention
source: heimdall lens template: infra
status: active
---
A tag is a name that can point at something else tomorrow. `latest` is a name that points at whatever was built last,
and a pipeline that uses it runs code nobody reviewed.

Pin an image by digest and an action by the commit it came from. A version tag is better than `latest` and is still
movable; a digest cannot move. The pin belongs in the repository, so the review sees it.
