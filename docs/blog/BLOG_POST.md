# We made AI reviewers prove it: building a merge gate on Switch

Agents write most of our code now. Our own build pull request was 77,460 added lines across 629 files. Nobody reads that. Someone types LGTM.

Tests do not save you either. In our first runs, an agent wrote a test that passed before the feature existed. It looked like coverage. It proved nothing.

We built Heimdall in four days to fix both problems. This post explains how it works, how we connected it to Switch, and what broke. If you are new to Switch, section 3 shows what we learned the hard way.

## 1. The two problems

**Review.** A large diff gets a fast approval. A generic AI reviewer adds comments, but it does not know why your team made a decision last year.

**Testing.** An agent can write tests that pass with any code. A green check then has no meaning.

Heimdall answers each one:

- A test agent writes tests from the ticket, before the code exists. The tests must fail first, and for the right reason.
- Reviewer agents file findings. The strongest finding is a test that fails on the commit.

A model never decides a merge. Ten rules in plain Go decide, and only a person can approve.

## 2. The system

The flow has seven stages: ticket, tests, pull request, review, proof, human, merge.

1. A Jira ticket moves to In Progress. Heimdall polls Jira and sees the move.
2. Heimdall opens a Switch room, creates a branch and wakes the test agent.
3. The agent submits tests. Heimdall runs them in a sandbox.
4. A developer opens a pull request. Reviewer agents start.
5. Each finding appears on the exact line, on the pull request and in chat.
6. The `heimdall/gate` check lists every rule that is not met.

In one run, on KAN-21, the room opened 0.9 seconds after Heimdall saw the ticket. The agent acknowledged the task after 23 seconds. A reviewer filed two findings 91 seconds after assignment.

## 3. Why Switch, and what a first-time user should know

Switch gives you three things that we did not want to build: rooms, agent identities and chat bridges.

- A **room** is one shared conversation with its history. Heimdall opens one room for each ticket.
- An **agent identity** gives each agent its own name and credentials. On our VM, each agent runs as its own Unix user.
- A **bridge** connects a room to a chat app. We connected Discord, Telegram and Mattermost. The team stays in the app it already uses.

These facts cost us time. Check them first.

**Your stream client must create the connection id.** Switch's agent event stream refuses a connection without `connection_id`. The client generates a UUID, sends it as a query parameter and reuses it on reconnect. We waited for the server to assign one, so our room stream never started.

**Heartbeats are floats.** `heartbeat_interval_seconds` arrives as `2.0`. Our decoder expected an integer and failed.

**Do not use `!` for your own commands.** Switch treats a post that starts with `!` as a Switch command. An unknown command gets "Unknown command" and goes no further. Our commands start with the plain word `heimdall`.

**Some bridges deliver only on a mention.** On Mattermost and Telegram, a message reached our coordinator only when it mentioned the coordinator. Our parser now ignores one leading mention.

**Invite an agent before you post to it.** Our test agent was never in its room, so it never woke. Now each task assignment invites the agent first. The invite is safe to repeat.

**Give each ticket a private room.** One agent session per ticket keeps its context clean. When two tickets shared a title, the second one adopted the first one's room. The room title now starts with the ticket key.

**A new environment variable may not reach your agent.** Switch's allowlist drops variables it does not know. We set the model provider in each agent's `opencode.json` instead.

## 4. Tests that must fail first

Most test generators stop when the tests pass. Heimdall does the opposite. It runs the tests against the code as it is today.

- If a test fails on an assertion, it is a good test.
- If a test fails to compile, Heimdall rejects it as a setup error.
- If a test passes, the feature cannot be what the test checks. Heimdall rejects it.

This is a real rejection from our event log:

```
TestPutNoteOnNonexistentRefundReturns404 passed without the feature on base
```

After verification, Heimdall commits the tests and records their git tree hash. A later change to those tests is visible to the gate.

There is one known gap. A fake test that imports the product can still pass this check.

## 5. Evidence, not opinion

A finding carries evidence, and evidence has tiers.

| Tier | Evidence | Can it block a merge? |
|---|---|---|
| E1 | A test that fails on an assertion | Yes |
| E2 | A deterministic tool result | Yes |
| E3 | A confirmed fact from team memory | Yes |
| E4 | Reasoning | Not alone |

This is a real reviewer's proof, shortened:

```go
_, err = svc.Refund(p.ID, 60)
if err != nil {
    t.Fatalf("first refund of 60 should succeed: %v", err)
}

_, err = svc.Refund(p.ID, 60)
if err == nil {
    t.Error("second refund of 60 must be rejected: cumulated 120 > payment 100")
}
```

We planted this bug on purpose, in a test repository. On KAN-20, an earlier run of the same ticket, the acceptance tests passed and a reviewer still proved this bug with its own failing test.

The gate is a pure function. It takes facts and returns every unmet rule:

```go
func Evaluate(f Facts) Decision {
    var unmet []Clause
    for _, c := range allClauses() {
        if reason := c.check(f); reason != "" {
            unmet = append(unmet, Clause{ID: c.id, Reason: reason})
        }
    }
    return Decision{OK: len(unmet) == 0, Unmet: unmet}
}
```

## 6. What broke

Fakes hid these problems. The real system found them, one at a time.

**The sandbox failed in six different ways.** We run tests in rootless Podman under a hardened systemd unit.

1. `NoNewPrivileges=true` stripped the setuid bit from `newuidmap`, so every job failed with `write to uid_map failed`.
2. The Podman store used `fuse-overlayfs`, but `PrivateDevices` hid `/dev/fuse`.
3. `runc` refused the memory, CPU and process limits. `crun` applied them.
4. A cold Go build filled the 64 MiB `/tmp`. We moved the build cache to a larger tmpfs.
5. `RestrictAddressFamilies` blocked loopback, so a test with a local HTTP server could never reach a red result.
6. `ProtectKernelTunables=true` made `/proc/sys` read-only for the Podman pause process. Every job joins that process, so every container failed after a restart.

Each job still runs with `--network=none`, a read-only root and `--cap-drop=ALL`.

**Our prompts never reached the model.** We denied file reads for agents, as a safety rule. That rule also blocked the agents from reading their own skill files. We now embed the prompts in the server and return them from `get_task`.

**A model wrote tool calls as chat text.** One agent printed its tool-call syntax as text and repeated it until the session held about 1.4 million input tokens. Nothing woke an agent that had already acknowledged its task. We added a stall nudge with a cap of four.

**We hit the GitHub rate limit.** Each room message caused a GitHub read, even plain chat. A failed message also restarted the stream every second. Now only real commands read identity. Stream retries back off. Repeated reads use ETags, because GitHub does not count a 304 against the limit.

## 7. Results

These numbers are from our VM on Nov 7, 2026.

| Measure | Value |
|---|---|
| Server memory, idle | about 42 MiB |
| Sandbox runner memory, idle | about 10 MB |
| VM | AWS r6a.large, 2 vCPU, 16 GB |
| Running on that VM | 9 containers, 7 agent units |
| Commits in four days | 241 |
| Unit tests at the last recorded run | 3,978 |

## 8. What is not done

Accuracy matters more than a good story, so here is the rest.

- Personal context packs are built and unit tested, but no pack ran on the live server. Today, the three reviewers use one prompt.
- Graphiti and FalkorDB run on the VM, but the graph is empty because delivery is blocked.
- We did not see three reviewers finish one pull request.
- We did not see Heimdall re-run a reviewer's proof end to end, or a full merge from agent-produced events.

The next step is to run those paths on real tickets, and then to write down what breaks.

Agents can write the code. They should also have to prove it.
