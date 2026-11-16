---
name: ui-testing
description: Write UI acceptance tests: start a preview of the base app to learn it, locate only by role and accessible name or label (the handles from the plan), stage tests that are red on base because the handles are not there, and treat exploration as never evidence. Use when the ticket declares a UI kind.
---

# Heimdall UI testing

You write the UI acceptance tests for one ticket before the implementation exists. A UI test is red on base
because the elements it names have not been added yet. The tests become a required CI gate, so they locate by
behaviour, never by styling.

## The one rule about locators

UI tests locate elements only by role and accessible name or label:

- a handle from the test plan, or
- something you saw in a snapshot of the base app.

A raw CSS or XPath selector is refused by the `locator-by-handle` lint. A handle is behaviour, not
implementation: what a screen reader announces is what the user gets. If you need a locator that is not a role
and a name, the plan or the app is wrong, not the lint.

## Flow

1. Start a preview of the base app (your task tells you how, and whether the profile declares a UI kind).
   Learn it: how to log in, how to reach the page the feature will live on, and what the surrounding controls
   are called.
2. Get the handles for the behaviours you are covering from the plan. If a behaviour has no handle, ask once,
   through the questions tool named by your task.
3. Write the tests and stage them one file at a time. They are red on base because the handles are not there:
   a missing element fails the test, which is the correct red. The failure must still be an assertion failure,
   never a collection or parse error.
4. Assert what a user can see: the submitted state, the visible message, the disabled control, the updated
   count. Never the DOM structure.
5. The handover lists the handles, so the developer knows the UI must have a button named Refund. Keep handle
   names stable and exact.

## On a head

The UI kind starts a preview on the head commit and tries to break a criterion. Exploration is never evidence.
What you file is a test that fails on an assertion at head, with the same locator rules.

## Determinism

- No sleeps and no raw timers. Rely on your tool's auto-waiting for the element you next assert on.
- No network beyond the app under test, no shared state between tests.
- Log in the way the seed or fixture provides; never hard-code a session.

## If the preview cannot run

If a browser cannot run inside the sandbox for this ticket, the UI kind is not declared for it and plans with
UI behaviours are refused with that reason. The sandbox is not loosened quietly.

## Accessibility

Accessibility is a check, not a hope: `axe-core` runs over the pages a UI test visits, and a violation there is
a finding on those pages. Prefer accessible names in your tests and you will catch most of it for free.
