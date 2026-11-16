# Lens packs

A lens is a folder of cards: the places a reviewer must look when a change touches a kind of thing. Heimdall ships
these templates in `deploy/lenses/`. The setup PR copies the packs a repository needs into `.heimdall/lenses/`. From
then on the repository owns them and edits them by pull request.

The packs here are the starting set named in `docs/REVIEW_ARCH.md` 5.2: `correctness`, `security`, `data`, `infra`,
`contract`, `operability`, `tests`. The `ui` pack comes later.

## Layout

```
deploy/lenses/<lens>/
  lens.yaml            # id, name, description, when
  <card>.md            # one card per file
  rules/
    <rule-id>.yaml     # one pattern rule per file
    testdata/
      <rule-id>.<ext>        # the file the rule fires on
      <rule-id>.clean.<ext>  # the file the rule must not fire on
```

`lens.yaml` has two fields the loader reads: `id`, which must equal the folder name, and `when`, the globs that make
the lens needed for a change. `name` and `description` are for the person reading the folder.

A card is a markdown file with a YAML block at the top, in the format `deploy/cards/README.md` documents:

```
---
applies_to: ["**/*.sql"]
triggers: ["ALTER TABLE"]
check: "Does the new column have a default, or is it nullable?"
proof: "Add a migration that adds the column to a table that already has rows."
kind: convention
source: heimdall lens template: data
status: active
---
```

`applies_to` is the repository-root glob the card applies to, `triggers` are literal strings in added lines that wake
it, `symbols` are code names that wake it, `check` is the one question that decides the card, and `proof` says how to
turn a violation into a test or a tool result. A lens card is a place to look. It is never evidence: it cannot reach
E3, only a person's confirmed card can.

## Rules

`rules/` holds small pattern rules, one per file, named after the rule id. They are written for the cards they serve
and are read like code: the pattern engine is the one named in `docs/REVIEW_ARCH.md` 4.2, and the rules use only what
it supports.

- The rule file is named after its rule id, because the engine's test mode finds the file it fires on by that name:
  `rules/<rule-id>.yaml` and `rules/testdata/<rule-id>.<ext>`.
- Every rule excludes `**/rules/testdata/**`. Without it the fixture that the rule fires on would be a finding in
  every scan of a repository that copied the pack.
- The rules use `pattern-regex`, so they are text rules. They do not parse the language, and they are leads, not
  enforced findings.

## Testing the rules

Each rule has one file it fires on and one it does not. `test-rules.sh` runs the engine's own test mode over every
pack and proves both halves:

    OPENGREP=/path/to/opengrep deploy/lenses/test-rules.sh

The script copies each pack's rules and fixtures into a temporary folder, runs `opengrep scan --test` there (the
engine compares the findings with the `ruleid:` annotations in the fixture) and then runs a plain scan to prove that
no `*.clean.*` fixture has a finding.

The engine is a build input, not part of this repository. The pinned binary belongs to the `checks` image
(`docs/REVIEW_ARCH.md` 4.2); the script takes it from `OPENGREP` or from `PATH`.
