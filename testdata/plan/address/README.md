# Rooms, Addressing, and Ticket Resolution (`testdata/plan/address/`)

This directory contains test inputs and expected parse results for message routing, addressee resolution, and ticket association.
Builders of M5 Interaction (step 3: `internal/domain/commands`, `app/announce`) must match these behaviors byte for byte.

## Specification Reference

- **`docs/AGENT_INTERACTION_ARCH.md`** section 3 F5 ("Rooms, channels and @") and section 13 H4 ("Rooms and addressing").
- **`docs/BUILD_PLAN.md`** section 4 M5 ("Interaction", step 3).

## Architecture Rules (H4)

1. **Ticket Association:**
   - Primary: Root of the thread if the message is in a change-anchored thread.
   - Secondary: Regex match of ticket key (`[A-Z]+-[0-9]+`, e.g. `KAN-22`) in the text.
2. **Addressee Resolution:**
   - Direct agent mention: `@reviewer-1`, `@test-author`, `@heimdall`.
   - Role mention: `@security` (maps to seat holding the security role), `@dev`, `@infra`.
   - Strips leading Slack / Switch mention wrappers (`<@U...>` or `@name`).
3. **Command Classification:**
   - `light` verbs: `start`, `review`, `remember`, `incident`, `challenge`, `answer` (execute directly).
   - `deciding` verbs: `uphold`, `dismiss`, `approve-tests`, `waive-reviewer`, `spec-change`, `resume`, `forget`, `override`, `reject` (require sender confirmation "yes").

## Test Cases (`cases.json`)

Includes test cases covering:
1. Thread reply with no explicit ticket in text -> associates with thread root ticket.
2. Top-level channel message naming ticket explicitly -> extracts ticket from text.
3. Role mention `@security` -> resolves to seat holding `security` role (`reviewer-2`).
4. Slack formatted mention `<@U0CFX056ZRA>` -> stripped cleanly.
5. Command classification: `light` vs `deciding` with confirmation requirement.
