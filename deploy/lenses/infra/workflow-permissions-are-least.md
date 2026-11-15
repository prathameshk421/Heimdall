---
applies_to: ["**/.github/workflows/**"]
check: "Does the workflow ask for the least permission it needs, and never run the code of a pull request with a write token?"
proof: "Show the permissions block of the workflow and the event that triggers it."
kind: convention
source: heimdall lens template: infra
status: active
---
The default token of a workflow may write to the repository. An event that runs the pull request's own code with
that token lets anyone who opens a pull request change the repository and read its secrets.

Ask for the least permission the job needs, and let it be no permission at all when the job only reads. Run the
untrusted code in a job whose token cannot write, and never in a job that holds a secret.
