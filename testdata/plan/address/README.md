# Addressing goldens (`testdata/plan/address/`)

M5 step 3 reproduces `expected` for every case in `cases.json`. These are the two functions H4 asks to test first:
**the ticket of a message** (thread root, key in text, neither) and **the board's "is this for me" rule**
(grammar, app mention, neither), plus which agent an `@` reaches.

Spec: `docs/AGENT_INTERACTION_ARCH.md` F3 (seats and roles), F5 (rooms, channels, `@`, the "What a mapped person
can type" table), 5.1 (three ways in), 5.3 (`propose_command` and verb classes), H4.

## Inputs

- `policy.yaml`: the seats and people that addressing reads, in the F3 shape (M0 step 7 builds `Policy.Seats`).
  `@security` reaches `reviewer-2` because that seat holds the role `security`. The values are F3's example.
- Each case's `input`: the channel (`tests` or `review`), the ticket of the thread root if the message is in a
  change's thread (`""` at the top level), the Switch sender id, whether the chat port says the board itself was
  mentioned (`mentions_board`), and the text.

## Expected

| Field | Values | Rule |
|---|---|---|
| `ticket`, `ticket_source` | the key and `thread_root`, `text` or `none` | F5 "One store, filtered by ticket": a thread message takes its root's change; a top-level message takes the key found in its text; otherwise there is none |
| `for` | `board`, `board_relay`, `agent`, `nobody` | 5.1 and F5: exact grammar (text starting with the plain word `heimdall` and a known verb) goes to the board; a mention of the board that is not grammar is relayed to the lead (F5, the Slack note); an `@` of an agent name or a role goes to that agent; text with no `@` reaches nobody |
| `agent` | `lead`, `test-author`, a seat id, or `""` | `@heimdall` is the lead; an agent name is itself; a role is the seat that holds it (F3) |
| `command` | `null` or `{verb, class, needs_confirmation}` | for grammar only. `class` is the 5.3 column. `needs_confirmation` is always `false` here: a typed command is authorized and run by the board with no model (5.1, F5 table); the "Reply yes" step is only for a command an agent proposes (5.3) |

A key in grammar is not part of the command: `heimdall uphold KAN-22 F1` names the ticket and runs `uphold F1`
(existing behaviour, `app/collaboration/commands.go` `withoutKey`).

## Cases

| id | Covers |
|---|---|
| `thread_role_mention_no_key` | F5's own example: `@security is F3 real?` in the KAN-22 thread, no key typed |
| `thread_agent_name` | an agent addressed by name in a thread |
| `thread_grammar_no_key` | grammar in a thread: the ticket comes from the root |
| `top_level_grammar_with_key` | grammar at the top level: the ticket comes from the text; a deciding verb, still no confirmation |
| `top_level_light_grammar` | a light verb |
| `top_level_lead_no_key` | the "neither" ticket case: no root, no key; the lead is still addressed |
| `thread_no_mention` | text with no `@` in a thread: nobody acts, the ticket is still known |
| `top_level_no_mention_no_key` | neither ticket nor addressee |
| `app_mention_not_grammar` | the board is mentioned with plain words: relayed to the lead |

Removed from the earlier version: `slack_raw_mention_stripped`, which assumed Switch passes Slack's raw
`<@U...>` form and that the board strips it. F5 says a Slack app mention "arrives as the board's name"; the raw
form and stripping are not documented (open question 2). Whether the sender is a mapped person is
authorization (`people.Authorize`), not addressing, so no case here depends on it.

## Open questions

1. **A thread message that names another ticket** (`heimdall uphold KAN-23 F1` inside the KAN-22 thread): does the
   root or the text win, or is it refused? F5 does not say. No case.
2. **How the board knows it was mentioned.** F5: "arrives as the board's name". The exact text form on each
   platform is spike SI2, not verified. The goldens take it as a chat-port flag, `mentions_board`.
3. **Grammar and a board mention together** (`@Heimdall heimdall uphold F1`): grammar or relay? No case.
4. **A role no seat holds, or two seats with one role**: F3 does not say. No case.
5. **Messages in a ticket's working room** (no thread root, the room is the ticket's): F5 describes the room but not
   how its messages are mapped. No case.
