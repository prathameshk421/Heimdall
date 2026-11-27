# Security policy

Heimdall is a merge gate that runs untrusted code in a sandbox. A security bug in Heimdall can let bad code reach a
default branch, so we treat every report with priority.

## Report a vulnerability

**Do not open a public issue, discussion or pull request for a security problem.**

Report it in private through GitHub:
[**Report a vulnerability**](https://github.com/prathameshk421/Heimdall/security/advisories/new) (the Security tab of
this repository).

Please include:

- The affected component and commit SHA.
- The steps to reproduce the problem, or a proof of concept.
- The impact as you see it.
- Your name or handle for the credit, if you want credit.

## What happens next

1. We confirm that we received the report within **7 days**.
2. We confirm or reject the problem, and tell you our plan and timeline.
3. We fix it in a private branch and publish a GitHub security advisory with the fix.
4. We credit you in the advisory, unless you ask us not to.

Please give us a reasonable time to fix the problem before you disclose it in public. We ask for 90 days, or less when
a fix ships earlier.

## Supported versions

Heimdall has no tagged release yet. We fix security problems on the `main` branch only.

## In scope

We are most interested in problems like these:

- **Gate bypass.** A pull request merges with a proven blocking finding, without a valid human approval, or with an
  approval on a different commit.
- **Policy tampering.** A pull request changes the policy that judges it (Heimdall reads policy at the merge base).
- **Sandbox escape.** Code under test reaches the network, the host filesystem, secrets or other jobs.
- **Secret exposure.** A model key, token or other secret appears in a log, a comment, a check run or an artifact.
- **Prompt injection with effect.** Text in a pull request makes an agent file a false proven finding, hide a real one,
  or act outside its tools.
- **Authentication and authorization.** Access to the dashboard, the MCP server or webhooks without valid credentials.
- **Supply chain.** A problem in the GitHub Action or its workflows, for example an unpinned dependency or a token
  with too much permission.

## Out of scope

- Findings from automated scanners with no proof of impact.
- Attacks that need an already compromised maintainer account or host.
- Denial of service by volume against a self-hosted install.
- Weak model output that does not bypass a rule (an unproven finding is hidden by design).

## Safe harbor

We will not take legal action against you for research that follows this policy. Test only against your own
installation or your own repositories. Do not access data that is not yours, and do not degrade a service for other
people.
