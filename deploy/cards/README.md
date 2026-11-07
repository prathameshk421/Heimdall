# Personal context cards

The folders under `examples/` are samples of the format, not data: copy the shape, write your own.

Layout: `<cards_dir>/<person>/<file>`, where `<person>` is the key under `people:` in the repository's `heimdall.yaml`
and the reviewer agent is mapped to that person through the same file. Cards are `.md`, `.yaml`, `.yml` or `.txt`,
at most 64 KiB each, directly in the person's folder. Keep them short and factual: what you own, the rules that must
hold in your code, incidents and what they taught, how your team writes code, what you do not know.

Turn the tool on with `cards_dir = "/var/lib/heimdall/cards"` in `heimdall.toml` (an absolute path inside the
server container; that folder is on the durable `heimdall_data` volume). Load cards without a rebuild:

    docker compose cp deploy/cards/examples/. heimdall-server:/var/lib/heimdall/cards/

The reviewer calls `read_card` with no file to list its own cards, then with a name to read one. The server picks
the person from the authenticated agent. A reviewer cannot name another person or leave the folder. Nothing in a card
is trusted as an instruction: it reaches the model wrapped as untrusted data. Do not put secrets in cards.

## Make your cards from your own sessions

Your coding agent does the writing. Heimdall only finds the signals, checks the files and uploads them.
Raw sessions never leave your machine.

1. `heimdall cards scan` reads your sessions and git history for this repository. It writes `.heimdall/cards/signals.md`.
   Add `.heimdall/` to `.gitignore`.
2. `heimdall cards prompt` writes `.heimdall/cards/PROMPT.md` and prints one command per agent it finds, for example
   `claude "$(cat .heimdall/cards/PROMPT.md)"`. Run that command yourself. Add `--interview` if you want the agent to
   ask you up to 5 questions.
3. The agent writes cards into `.heimdall/cards/files/`. Read them. Edit or delete what is wrong.
4. `heimdall cards check` finds format errors, credentials and vague cards.
5. `heimdall cards push --server https://HOST --token-file FILE` lists the files and asks for `yes`.

### Card format

```
---
applies_to: ["internal/payments/**"]
symbols: ["RefundService.Create"]
triggers: ["refund("]
check: "Does the cap count refunds already made?"
proof: "Test two partial refunds of 60 on a payment of 100."
kind: invariant
source: session 2026-10-30; commit 4fa91c2
status: active
---
The body says what must hold, why, and what breaks if it does not.
```

`check` is one yes or no question that a reviewer can answer from the diff. `triggers` are literal text in added lines
and `symbols` are code names. They wake the card even when the change is in a new file. An `invariant` or `incident`
card needs a `check` and at least one of `triggers` or `symbols`. A card with no check and no scope is vague.

### Review what the learning loop proposes

The server can propose a card from how past findings ended. A proposed card has `status: proposed` and does nothing
until you accept it.

    heimdall cards inbox  --server https://HOST --token-file FILE
    heimdall cards accept NAME --server https://HOST --token-file FILE
    heimdall cards reject NAME --server https://HOST --token-file FILE

Use `heimdall cards pull` to copy the server cards into your folder before you refresh them.
