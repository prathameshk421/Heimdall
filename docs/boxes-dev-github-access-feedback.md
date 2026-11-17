Subject: boxes.dev agent-init: GitHub access for collaborators - two issues and three suggestions

Hi boxes.dev team,

I set up the Heimdall project (prathameshk421/Heimdall, previously
flip_the_switch_hackathon) on a Template box using `dvb agent-init` with
CLI 1.0.1122. The flow mostly worked well - the SSH key setup/verify
handshake in particular was clean - but I hit two related GitHub access
issues that I think are worth fixing, and that point at a missing
capability. Details below.

SETUP CONTEXT
- boxes.dev account: oakaarush@gmail.com
- Connected GitHub account: aarush323
- Repository owner: prathameshk421 (a different personal account)
- aarush323 is a collaborator on that repo with push rights (verified via
  the GitHub API: permissions.push = true, permissions.admin = false)
- Template box transport chosen: SSH (githubAccess: ssh), which worked

ISSUE 1: A RENAMED REPOSITORY BREAKS THE ORIGIN MATCH

The local checkout's origin still pointed at the pre-rename name
(git@github.com:prathameshk421/flip_the_switch_hackathon.git) while the
repository had been renamed to prathameshk421/Heimdall.

The CLI detected this and printed the exact fix, which was genuinely
helpful:

  "prathameshk421/flip_the_switch_hackathon is now prathameshk421/Heimdall
   on GitHub, so boxes.dev can't match this project's origin to it.
   From the project folder, update the origin to the current name with
   `git remote set-url origin git@github.com:prathameshk421/Heimdall.git`."

The suggestion to have the user edit the remote by hand is where I think
it can be better. GitHub redirects renamed repositories, so the canonical
identity could be resolved automatically and applied to the local origin.
Two benefits: the rename becomes transparent instead of a manual step,
and downstream matching (App access, maintenance watch baselines,
generated devbox startup scripts) keys off a stable repository id instead
of a string that goes stale on every rename.

ISSUE 2: THE "ASK THE REPO OWNER" ADVICE IS DERIVED FROM A ROLE, NOT A
CAPABILITY

After the rename, `dvb agent-init github connect` reported:

  "prathameshk421/Heimdall belongs to @prathameshk421's personal account.
   Ask @prathameshk421 to give boxes.dev access to it on GitHub."

This is reasonable as far as it goes, but it is advice about the
GitHub App specifically, presented as if it were the only way forward. In
my case it was not:

- The GitHub App can only ever cover a repository the connected account
  owns or can be granted. A collaborator with push rights can never
  obtain App access without the owner's action, by construction.
- I already had push rights on the repository through my own account, and
  the SSH transport carried those rights over to the Template box with no
  owner involvement and no extra permissions.

So the situation was not "you need the owner" but "you already have
everything you need, through a different transport". The CLI had the
facts available (it knew the repo owner, and a credential check would
have shown that the connected account has push access) but the message
inferred a role - connected account equals repo owner - instead of
testing a capability.

Two suggestions here:

(a) Make the transports peers rather than a ladder of failure. The
    current framing is "default is the managed App, SSH is the
    fallback". For a collaborator, SSH is not a fallback, it is the
    correct default. Choosing between "product-managed credential" and
    "the credential you already have" should be framed as "which
    credential should the box use?", with the recommendation derived from
    the actual relationship:
      - connected account owns the repo  -> managed App (no key management)
      - connected account has collaborator rights -> your own SSH key
      - neither -> request access, or use a token
    Capability-derived defaults would have avoided the detour entirely.

(b) Make the error message capability-driven. Something like "Your
    connected account has push access to this repo but is not its owner.
    App access requires the owner. SSH works now; App additionally enables
    automatic pull-request updates and follow-ups." states what the user
    can actually do, and keeps the App as an upgrade rather than a
    gate.

SUGGESTION 3: A MISSING TRANSPORT - THE USER'S OWN TOKEN

There are currently two transports, and neither fits a collaborator who
does not want to involve the owner: the managed App (owner-only) and SSH
(a key the user registers on their own account). A third option is
missing - the user's own repository-scoped credential, for example a
fine-grained personal access token limited to that one repository.

That would have solved my case immediately, with no owner action and no
broader permissions than SSH, and it would also give boxes.dev a way to
grant narrow, revocable, per-repository access instead of an
account-wide App installation. The platform already carries secrets
(managed environment files, agent permissions), so the plumbing appears
to exist.

SUGGESTION 4: MAKE THE OWNER ASK SMALLER

When the owner does need to act, the request should be for the minimum:
install the App on only that repository ("Only select repositories"), not
on the account. The current framing risks asking for an account-wide
install, which is a much larger concession and much less likely to be
granted. If boxes.dev generates that request message, it should name
the single-repository option explicitly.

WHAT WORKED WELL, FOR THE RECORD
- The SSH flow end to end: key generation, the pending/registered/verified
  handshake, and verification against the correct repository were all
  correct and fast.
- The rename detection produced a precise, correct diagnostic with the
  exact command to fix it.
- The manifest-based agent-driven setup model itself worked well - the
  manifest is a readable contract and the sections map cleanly onto what
  a project actually needs.

Happy to test any of this if you want a second pair of eyes on the
flows. Happy to send my manifest if that is useful.

Thanks,
Aarush
