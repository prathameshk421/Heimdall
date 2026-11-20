# 10. Open-source building blocks for a local-first developer context pack

Date of research: 2026-11-21. Method: the GitHub REST API through `gh` for stars, licence, language, last release and archive state. All counts were read on 2026-11-21, one call per repo. I read about 20 README and source files, used the arXiv API for paper ids and abstracts, and read vendor docs for the rule-file formats. No paid source and no private repo was used.

Source rules for this file:
- Every star count is from the GitHub API on 2026-11-21. Star counts move fast. Treat them as a snapshot, not a rank.
- "Last release" is the latest non-draft GitHub release on 2026-11-21. "No release" means the repo publishes no GitHub release. It may still ship tags or packages.
- "Not verified" means I found no source I trust. I did not guess.
- Licence is the SPDX id GitHub reports for the repo root.

---

## 0. The shape this research assumes

The product is a CLI on a developer laptop. It does five things.

1. Reads coding-agent session logs and the local git history.
2. Extracts durable rules and decisions, each with a citation to a session turn or a commit.
3. Redacts secrets and personal data before anything leaves the laptop.
4. Shows each candidate rule to the developer for approval.
5. Shares the approved pack with a team server and with AI code reviewers.

The filter is the same for every item. Can a Go single binary use it offline, under a permissive licence, with no server to run? Section 7 gives the verdict.

---

## 1. Session log formats and readers

### 1.1 The formats as they exist today

| Agent | Where the log lives | Format | Stability |
|---|---|---|---|
| Claude Code | `~/.claude/projects/<project>/<session>.jsonl` | JSONL, one turn per line | No stable public spec. Version it per file. Source: [claude-code-log README](https://github.com/daaain/claude-code-log) |
| OpenAI Codex CLI | `$CODEX_HOME/sessions` and `archived_sessions`, `~/.codex` by default | JSONL rollout files | Code constant `SESSIONS_SUBDIR` in [rollout/lib.rs](https://github.com/openai/codex/blob/main/codex-rs/rollout/src/lib.rs) |
| OpenCode | `~/.local/share/opencode/opencode.db`, override with `OPENCODE_DB` | SQLite | Documented in [troubleshooting.mdx](https://github.com/anomalyco/opencode/blob/dev/services/site/src/docs/content/troubleshooting.mdx) |
| Cursor | `state.vscdb` under `workspaceStorage/*/` and `globalStorage/` | SQLite, plus rich text blobs | Closed source. Read by [cursor-history](https://github.com/S2thend/cursor-history) |
| Aider | `.aider.chat.history.md` | Markdown | Documented option `--chat-history-file`. Source: [options.md](https://github.com/Aider-AI/aider/blob/main/aider/website/docs/config/options.md) |
| Continue | `~/.continue/sessions` | JSON | Source: [paths.ts](https://github.com/continuedev/continue/blob/main/core/util/paths.ts) |
| Cline | `<globalStorage>/<ext>/tasks/<id>/api_conversation_history.json` plus `task_metadata.json` | JSON per task | Source: [disk.ts](https://github.com/cline/cline/blob/main/apps/vscode/src/core/storage/disk.ts) |

Three formats cover most users: Claude Code JSONL, Codex JSONL and OpenCode SQLite. Cursor is the expensive one. It is closed, it splits state between workspace and global stores, and it changes without notice.

### 1.2 Readers and exporters

| Project | Stars | Licence | Language | Last release | What it does | How to use it |
|---|---|---|---|---|---|---|
| [SpecStory](https://github.com/specstoryai/getspecstory) | 1,377 | Apache-2.0 | Go | 2026-11-06, v2.17.0 | Captures Claude Code, Codex CLI, Cursor IDE and CLI, Cursor Cloud, Droid and Gemini CLI into `.specstory/history`. Ships `Lore`, which mines sessions into reusable skills | The strongest base for the reader layer. Same language, permissive licence, local first, multi-agent. Read its adapters before writing your own |
| [claude-code-log](https://github.com/daaain/claude-code-log) | 1,269 | MIT | Python | 2026-11-15, 1.8.0 | Converts Claude Code JSONL to HTML, Markdown and JSON. Codex and Antigravity are beta providers | Use the JSON output as a second opinion on the transcript schema. Do not ship a Python runtime for this alone |
| [ccusage](https://github.com/ccusage/ccusage) | 19,524 | MIT | Rust | 2026-11-14, v20.0.51 | Reads session logs and reports token use and cost | Use cost and turn count to rank which sessions are worth mining. Not needed for rule text |
| [OpenCode](https://github.com/anomalyco/opencode) | 228,212 | MIT | TypeScript | 2026-11-17, v1.19.6 | The agent itself. Sessions are rows in SQLite | Read the database. Do not parse its internal files |
| [Codex CLI](https://github.com/openai/codex) | 135,986 | Apache-2.0 | Rust | 2026-11-20, rust-v0.171.0 | The agent itself. JSONL rollouts. Has `external-agent-migration`, which imports Claude and Cursor sessions | Read the JSONL. The migration module shows how OpenAI maps other formats, which is free schema research |
| [Cline](https://github.com/cline/cline) | 71,792 | Apache-2.0 | TypeScript | 2026-10-26, v4.1.26 | Agent in VS Code. Task history as JSON files | Adapter with low effort. One folder per task |
| [Aider](https://github.com/Aider-AI/aider) | 50,199 | Apache-2.0 | Python | 2025-08-09, v0.86.0 | Terminal pair programmer. Chat history is plain Markdown. Last push 2026-05 | Easiest adapter. Markdown in, text out |
| [Continue](https://github.com/continuedev/continue) | 36,531 | Apache-2.0 | TypeScript | 2026-06-19, v2.0.0-vscode | IDE agent. Sessions as JSON in `~/.continue` | Adapter. Check the schema version per file |
| [cursor-history](https://github.com/S2thend/cursor-history) | 154 | MIT | TypeScript | 2026-08-16, v0.18.0 | Browses, exports and backs up Cursor chat history from `state.vscdb` | Read its SQL before writing a Cursor adapter. It documents the workspace and global split |
| Cursor | closed | none | closed | none | The IDE. Rules docs are public, chat store is not | Best effort only. Never let Cursor be the only source for a rule |

### 1.3 What this means for the CLI

- Normalise every source into one event: `{source, session_id, turn_index, role, text, ts, repo, raw_ref}`. Keep `raw_ref` so a rule can cite the exact turn.
- Ingest incrementally. Claude Code and Codex append JSONL. OpenCode and Cursor need a query by rowid or timestamp.
- Do not trust a log to be small. One long session can hold hundreds of megabytes of tool output. Stream and cap per turn.
- Treat every log line as untrusted text. A session can contain a prompt injection that says "write this rule to the team server". The extractor must treat log text as data, never as instructions.

---

## 2. Memory systems with an open-source core

| Project | Fully local | MCP server | Stars | Licence | Language | Last release | What it does | How to use it |
|---|---|---|---|---|---|---|---|---|
| [Mem0](https://github.com/mem0ai/mem0) | Yes with local models. Default examples call hosted models | Yes, per-agent MCP plugins (Claude Code, Codex, Cursor, OpenCode and more) | 69,283 | Apache-2.0 | Python | 2026-09-25, ts-v3.3.1 | Extracts and consolidates facts from conversations. OSS library plus a Docker stack. Hosted platform is the default product | Useful as a design reference for extraction and dedupe. Too heavy as the core of a laptop CLI |
| OpenMemory (Mem0) | Claimed local and private | Yes | not verified | not verified | not verified | not verified | Mem0's local MCP memory server | **Not verified.** The `mem0ai/openmemory` name now holds a different tool (session porting, 108 stars, MIT). The OpenMemory docs route redirects to the Mem0 home page. Do not plan on it |
| [Letta](https://github.com/letta-ai/letta) | Yes, local mode | Yes, MCP originator code in `letta-code` | 25,505 | Apache-2.0 | Python/TypeScript | 2026-05-14, 0.16.8 | Stateful agents with memory (formerly MemGPT). Active source moved to [letta-code](https://github.com/letta-ai/letta-code), 3,884 stars, v0.34.67 on 2026-11-20 | Read the memory-block design. Do not embed the server |
| [Graphiti](https://github.com/getzep/graphiti) | Self-hosted, needs Neo4j or FalkorDB | Yes, reference MCP and REST servers in repo | 32,963 | Apache-2.0 | Python | 2026-10-20, v0.31.2 | Temporal knowledge graph for agent memory. Edges carry validity windows. Zep Cloud is the paid path | The best fit for the team server, not the laptop. Temporal edges match "this rule held until commit X" |
| [Cognee](https://github.com/topoteretes/cognee) | Yes, local by default, no API key claimed | Yes, `cognee-mcp` | 33,433 | Apache-2.0 | Python | 2026-11-18, v1.6.10 | Builds a memory graph from documents, code and conversations. Local UI launcher | Optional team-side index. Python service, so not the CLI |
| [LangMem](https://github.com/langchain-ai/langmem) | In-process, so local. Default store is in-memory | No | 1,779 | MIT | Python | No release | Extracts memories and optimises prompts. Needs a LangGraph `BaseStore` | Steal the extraction prompt shapes. No Go path |
| [Basic Memory](https://github.com/basicmachines-co/basic-memory) | Yes. Plain Markdown on disk | Yes | 4,647 | AGPL-3.0 | Python | 2026-10-29, v1.0.0 | Local knowledge base as Markdown files with an MCP server | Good file-layout idea. **AGPL is a real problem** for a shipped CLI that links it |
| [claude-mem](https://github.com/thedotmack/claude-mem) | Partly. Hosted memory is the default, local observer is opt-in | Yes, MCP search tools | 118,595 | Apache-2.0 | TypeScript | 2026-11-20, v16.0.2 | Hooks capture sessions, compresses them with a model, injects context into later sessions. Works with many agents | Study the hook UX and the capture points. Do not take the hosted default |
| [Memobase](https://github.com/memodb-io/memobase) | Local server, needs Postgres and Redis | Yes, `src/mcp` | 2,962 | Apache-2.0 | Python | No release | User-profile memory for chat apps. FastAPI plus Docker | Profile schema is a useful reference. Too many services for a laptop |
| [MemOS](https://github.com/MemTensor/MemOS) | Local plugin uses SQLite. Self-host core needs Neo4j and Qdrant | Not verified | 12,465 | Apache-2.0 | TypeScript | 2026-11-09, v2.0.36 | Tiered memory (traces, policies, world model) plus a local viewer. Claims 100% local in plugin mode | The L1 trace to L2 policy split is close to "turn logs into rules". Read the design, skip the stack |
| [A-MEM](https://github.com/agiresearch/A-mem) | Yes, research code, Ollama option | No | 1,217 | MIT | Python | No release | Zettelkasten-style linked memory notes. Each note gets keywords, tags and links | Copy the linking idea. Code is a paper artefact, not a product |
| [Supermemory](https://github.com/supermemoryai/supermemory) | Yes, self-host. One binary. Offline with Ollama | Yes | 31,675 | MIT | TypeScript | 2026-10-12, server-v0.0.9 | Memory and context engine with a self-host path and an MCP server | Closest OSS shape to a team memory server. Still a TypeScript service |

Two findings matter more than any single row.

1. Every serious project here is Python or TypeScript. None gives a Go library for extraction. A Go CLI must own that code.
2. The products that win attention (Mem0, claude-mem, Supermemory) default to a hosted service. The local path is the second-class path. That is the gap a local-first CLI can take.

---

## 3. Secret and PII redaction usable offline

| Project | Stars | Licence | Language | Last release | What it does | How to use it |
|---|---|---|---|---|---|---|
| [gitleaks](https://github.com/gitleaks/gitleaks) | 31,219 | MIT | Go | 2026-03-21, v8.30.1 | Regex and entropy detection over files, git history and stdin, with a large maintained rule set (rule count not verified). `--redact` masks secrets in its own report and stdout, not in your files | Best Go detector. Call the library in process. Write your own replacer, because a detector only reports |
| [TruffleHog](https://github.com/trufflesecurity/trufflehog) | 29,548 | **AGPL-3.0** | Go | 2026-11-19, v3.100.0 | About 800 detectors. Can verify a key by calling the provider | Avoid in a shipped binary. AGPL plus network calls to third parties is a bad fit for a local-first tool |
| [detect-secrets](https://github.com/Yelp/detect-secrets) | 4,682 | Apache-2.0 | Python | 2024-05-06, v1.5.0 | Baseline plus audit workflow. Plugins per secret type. Last push 2026-04 | Copy the baseline and allowlist idea. Release is two years old |
| [Presidio](https://github.com/data-privacy-stack/presidio) | 11,989 | MIT | Python | 2026-10-27, 2.2.366 | PII detection and anonymisation across text, images and structured data. NLP plus patterns, offline with local spaCy models | The reference for PII classes and operators. Too heavy to embed (Python plus models) |
| [scrubadub](https://github.com/LeapBeyond/scrubadub) | 432 | Apache-2.0 | Python | 2023-09-01, v2.0.1 | Removes names, emails, phone numbers, addresses from text. Last push 2023 | Read its regex list. Do not depend on it |
| [ferret-scan](https://github.com/awslabs/ferret-scan) | 104 | Apache-2.0 | Go | 2026-11-20, v2.6.1 | Offline PII, secret and credential detection with format-preserving redaction. `pkg/scan` and `pkg/redact`. Single static binary, stdin mode | **Best Go fit for PII.** Young and low-star, so pin a version and test its rules |
| [deidentify](https://github.com/aliengiraffe/deidentify) | 41 | MIT | Go | 2026-08-11, v1.0.4 | Replaces PII with deterministic, format-preserving substitutes | Use for consistent pseudonyms across a pack. Small and single-purpose |
| [secretlint](https://github.com/secretlint/secretlint) | 1,502 | MIT | TypeScript | 2026-11-16, v14.0.1 | Pluggable credential linter with a broad rule set | Read the rule list. Not a Go dependency |

Design notes for the redaction step:

- **Two passes, not one.** Pass 1 removes secrets (keys, tokens, passwords, connection strings). Pass 2 removes personal data (emails, names, phone numbers, internal hostnames). One pass with one rule set misses both.
- **Detect then replace with a typed placeholder.** `<<SECRET:aws_access_key:1>>` beats `[REDACTED]`. It shows the reader what was removed and lets the team server refuse a pack that still contains the placeholder in a code block.
- **Redact before the model sees the text.** A rule extractor that reads raw logs sends secrets to a model. Redact on ingest.
- **Do not upload raw sessions at all.** Upload the rule, its citation span and a hash of the session. The team server does not need the transcript.
- **Test with real shapes.** gitleaks and ferret-scan both have rule fixtures. Keep your own corpus of fake keys so a rule change cannot silently stop redacting.

---

## 4. Rule-file conventions agents read

| File | Who reads it | Notes | Source |
|---|---|---|---|
| `AGENTS.md` | Codex, Cursor, GitHub Copilot, Gemini CLI (configured), Aider (configured), OpenCode, Zed, Warp, Jules, Amp, Windsurf/Devin | Plain Markdown, no frontmatter. Nearest file in the tree wins. Used by over 60k open-source projects. Stewarded by the Agentic AI Foundation under the Linux Foundation | [agents.md](https://agents.md), read 2026-11-21 |
| `CLAUDE.md` | Claude Code. Copilot also accepts it | Claude Code memory file. Nested files are read | [Claude Code docs](https://docs.claude.com/en/docs/claude-code/memory), and the Copilot page below |
| `.cursor/rules/*.mdc` | Cursor | Frontmatter `description`, `globs`, `alwaysApply`. A plain `.md` in that folder is ignored | [Cursor rules](https://cursor.com/docs/context/rules), read 2026-11-21 |
| `.github/copilot-instructions.md` | GitHub Copilot | Repository-wide. Path-specific files are `.github/instructions/NAME.instructions.md` with `applyTo` globs. Copilot also reads the nearest `AGENTS.md`, or a root `CLAUDE.md` or `GEMINI.md` | [GitHub docs](https://docs.github.com/en/copilot/how-tos/configure-custom-instructions/add-repository-instructions), read 2026-11-21 |
| `.windsurfrules`, `.windsurf/rules/*.md`, `.windsurf/global_rules.md` | Windsurf, now under Devin | Legacy files are still imported by default. `.devin/rules/*.md` is the new path | [Devin local customizations](https://docs.windsurf.com/windsurf/cascade/memories) (redirects to docs.devin.ai), read 2026-11-21 |
| `GEMINI.md` | Gemini CLI | Default context file name. Configurable with `context.fileName`. Also read from `~/.gemini/GEMINI.md` | [configuration.md](https://github.com/google-gemini/gemini-cli/blob/main/docs/reference/configuration.md) |
| `CONVENTIONS.md` | Aider | Loaded read-only with `/read CONVENTIONS.md` or `aider --read`. Aider can load `AGENTS.md` with `read: AGENTS.md` in `.aider.conf.yml` | [Aider conventions](https://github.com/Aider-AI/aider/blob/main/aider/website/docs/usage/conventions.md) |
| `.clinerules` | Cline | Directory or single file, per Cline docs | [Ruler support table](https://github.com/intellectronica/ruler) |

Two facts change the design.

1. `AGENTS.md` is now the common root. Cursor, Copilot, Gemini CLI and Aider all read it, some after a setting. If the pack writes one file, write `AGENTS.md`. Generate the other files only for users who ask.
2. Copilot reads custom instructions and `AGENTS.md` from the **head branch**, not the base. A pull request can therefore change the rules that review it. This matches the known attack surface in paper 2610.38521 below. A reviewer-facing pack must be read from the merge base, or signed, or both.

### Converters between formats

| Project | Stars | Licence | Language | Last release | What it does | How to use it |
|---|---|---|---|---|---|---|
| [rulesync](https://github.com/dyoshikawa/rulesync) | 1,633 | MIT | TypeScript | 2026-11-20, v43.11.0 | Generates and imports rules, skills, agents, MCP config and hooks for about 20 tools. `rulesync convert --from cursor --to copilot,claudecode` | Use as the format matrix and test oracle. Reimplement the small mapping in Go |
| [Ruler](https://github.com/intellectronica/ruler) | 2,993 | MIT | TypeScript | 2026-10-22, v0.3.46 | One `.ruler/` source, applied to each agent's file. Table lists the target file per tool | Read the target table. Copy the model, not the dependency |
| [awesome-cursorrules](https://github.com/PatrickJS/awesome-cursorrules) | 41,173 | CC0-1.0 | JavaScript | No release | A large collection of Cursor rule examples | Example corpus for tests. Not code |
| [agent-rules](https://github.com/steipete/agent-rules) | 5,668 | MIT | Shell | No release | Rules and knowledge for agents. **Archived** | Skip |

---

## 5. Signing and sync

| Project | Stars | Licence | Language | Last release | What it does | How to use it |
|---|---|---|---|---|---|---|
| [cosign](https://github.com/sigstore/cosign) | 6,456 | Apache-2.0 | Go | 2026-10-20, v3.1.4 | Signs and verifies blobs (`sign-blob`, `verify-blob`) and containers. Keyless mode uses Fulcio and the Rekor transparency log | Use key-based signing offline. Keyless needs the network, so it cannot be the default for a local-first flow |
| [age](https://github.com/FiloSottile/age) | 24,394 | BSD-3-Clause | Go | 2026-08-29, v1.3.2 | File encryption with X25519 keys and scrypt passphrases. Small explicit keys, no config. Usable as `filippo.io/age` | Encrypt the pack to the team's public keys. Best Go building block in this section |
| [minisign](https://github.com/jedisct1/minisign) | 2,896 | ISC | C | 2025-01-15, 0.12 | Ed25519 detached signatures over files. Tiny and simple | Good if you want a 100-line verifier in any language. No official Go library (not verified) |
| OpenSSH signatures | n/a | n/a | C | ships with OpenSSH | `ssh-keygen -Y sign` and `-Y verify` sign a file with an existing SSH key | Zero new key management. Developers already have keys. Signature format is documented in OpenSSH PROTOCOL.sshsig |
| [restic](https://github.com/restic/restic) | 37,322 | BSD-2-Clause | Go | 2026-11-19, v0.20.0 | Encrypted, deduplicated backup to S3, SFTP, local disk | A ready transport for packs. Do not build your own blob store first |
| [syncthing](https://github.com/syncthing/syncthing) | 90,637 | MPL-2.0 | Go | 2026-10-21, v2.1.7 | Peer-to-peer folder sync with device identities | Optional path for teams with no server. MPL is file-level copyleft, so link carefully |

Simple sync patterns that fit this product, cheapest first:

1. **Git repo of signed JSON.** One file per rule, one index file. The team server is a bare repo. Review, diff, revert and blame come free. Sign each commit with an SSH key.
2. **Content-addressed blobs plus a manifest.** The CLI writes `sha256` blobs and a signed manifest. The server stores blobs by hash. This dedupes well and gives an audit trail.
3. **Append-only log with a hash chain.** Each entry names the previous hash. This is the strongest tamper evidence and the most work to query.
4. **Encrypted backup.** `age` the pack, push with `restic` to object storage. Good for a small team, poor for per-rule permissions.

Do not start with a CRDT or real-time sync. Rules change slowly and a human approves each one. Last-writer-wins with a signed timestamp is enough.

---

## 6. Papers, 2024 to 2026

Ids are arXiv. Dates are first submission.

### 6.1 Learning reusable rules from review feedback

| Id | Date | Finding | Why it matters here |
|---|---|---|---|
| [2607.13091](https://arxiv.org/abs/2607.13091) | 2026-07-13 | Codifies every accepted review comment as a persistent behavioural rule in a version-controlled instruction file, plus a self-review checklist. On a 35+ service platform the set grew from 5 to 18 rules. Reports 0% recurrence for ruled-against error classes across 11 recorded sessions | The closest published version of this product. Cite it, and copy its loop: accepted comment, rule, checklist, validation. Note the limits: one company, 11 sessions, self-reported |
| [2607.03316](https://arxiv.org/abs/2607.03316) | 2026-07-03 | Mines 31,073 review and developer-feedback pairs from 10,191 PRs in 239 repos for CodeRabbit reviews | The best public corpus for "which review comments are accepted". Use it to choose which comments become rules |
| [2607.16740](https://arxiv.org/abs/2607.16740) | 2026-07-18 | Trajectory-level analysis of agentic review in the terminal: behaviour, cost and human alignment | Cost and noise data for reviewers. Use it to set a rule budget per review |
| [2611.23476](https://arxiv.org/abs/2611.23476) | 2026-11-17 | GUARD: self-editing harnesses regress, forget skills and bloat memory without an external grader. Uses adaptive rule distillation and regret guidance | A warning for auto-extraction. Every added rule needs a test or a human, or the pack rots |

### 6.2 Instruction files and their risks

| Id | Date | Finding | Why it matters here |
|---|---|---|---|
| [2611.02383](https://arxiv.org/abs/2611.02383) | 2026-11-02 | Empirical study of 3,387 instruction files from 1,000 repos across AGENTS.md, CLAUDE.md and two other formats. Measures adoption, consistency and maintenance | The evidence base for format choice. Use its categories as the default rule schema |
| [2610.38521](https://arxiv.org/abs/2610.38521) | 2026-10-28 | Malicious memory files such as CLAUDE.md change secure-completion rates by -8 to +6 points, with no consistent pattern. Claude Code named the file in about two thirds of final messages; OpenCode mentioned it mainly in reasoning | Do not overclaim the attack. Do claim the disclosure gap. Supports reading policy from the merge base and showing the source file on every rule |
| [2611.24165](https://arxiv.org/abs/2611.24165) | 2026-11-18 | MemoSync: formative study with 12 developers on what agents store, retrieve and apply. Builds co-management of metamemory | Direct input for the approval UI. Developers want to see and edit what is remembered, not just a list |
| [2611.20927](https://arxiv.org/abs/2611.20927) | 2026-11-16 | LEDGER: model proposes facts, explicit rules decide which value is current, which source to trust and what to delete. Each fact keeps source and validity period | The right model for a rule store: rules, not embeddings. Validity periods fit "this held until commit X" |
| [2611.22017](https://arxiv.org/abs/2611.22017) | 2026-11-17 | SkillBuffBench: skills help much less in deployment, where the agent picks from a library and carries memory, than in hand-picked benchmarks | Set expectations. Measure the pack against a control, not against a demo |

### 6.3 Memory and context from experience

| Id | Date | Finding | Why it matters here |
|---|---|---|---|
| [2510.04618](https://arxiv.org/abs/2510.04618) | 2025-10-06 | ACE: contexts as evolving playbooks. Names brevity bias and context collapse, where repeated rewriting erodes detail | Design the pack as append-and-merge, never as "summarise the file each week" |
| [2409.07429](https://arxiv.org/abs/2409.07429) | 2024-09-11 | Agent Workflow Memory: induce reusable routines from past trajectories and select them per task | The pattern for turning repeated session steps into a rule |
| [2507.06229](https://arxiv.org/abs/2507.06229) | 2025-07-08 | Agent KB: shares problem-solving experience across agent frameworks | Prior art for a team-shared pack that different agents consume |
| [2504.19413](https://arxiv.org/abs/2504.19413) | 2025-04-28 | Mem0: extraction, consolidation and retrieval of salient facts, plus a graph variant | The baseline your extractor will be compared to. Its dedupe step is the part worth copying |
| [2308.10144](https://arxiv.org/abs/2308.10144) | 2023-08-20 | ExpeL: agents gather experience and extract insights without weight updates | Older, but the clearest statement of the "insights, not weights" approach |
| [2605.18284](https://arxiv.org/abs/2605.18284) | 2026-05-18 | CommitDistill: distils commit messages, PR discussion and issues into a typed repository memory | The git-history half of the product. Use its typing, not its model stack |
| [2610.34098](https://arxiv.org/abs/2610.34098) | 2026-10-25 | TRACER: uses an agent's own development history and structural diffs to find missing guards in agent PRs | Shows the session log is evidence a reviewer can use, not just training data |

---

## 7. Verdict

### 7.1 Build on these five

| Pick | Why |
|---|---|
| **SpecStory** (Go, Apache-2.0) | It already solves the worst part: many agents, many formats, local first, one output. It is Go, permissive, and it ships the "sessions to skills" idea. Either vendor its adapters or depend on the CLI and parse `.specstory/history` |
| **gitleaks as a library** (Go, MIT) | The best maintained detector, in the right language, with a rule format you can extend. Use it for pass 1. Its `--redact` flag is not a redactor, so write the replacer yourself |
| **ferret-scan** (Go, Apache-2.0) | The only offline Go library found with PII detection and format-preserving redaction (`pkg/scan`, `pkg/redact`). It is young and small, so pin it and test its rules. This covers pass 2 |
| **age** (Go, BSD-3-Clause) | The smallest correct way to encrypt a pack to a team's public keys, with no service and no config. `filippo.io/age` drops into a Go CLI. Use key-based cosign or SSH signatures only if you need public verifiability |
| **AGENTS.md as the output format** | It is read by Codex, Cursor, Copilot, Gemini CLI, Aider, OpenCode and more. 60k+ repos use it. Writing one file beats writing seven. Use rulesync's table as the spec for the other formats, and read policy from the merge base |

Also build, but small: a SQLite store (pure-Go driver) for rules, citations and approvals. A rule is a row with `text`, `scope`, `source_ref`, `source_hash`, `author`, `approved_by`, `approved_at`, `supersedes`. Nothing here needs a vector database on day one.

### 7.2 Avoid

| Avoid | Reason |
|---|---|
| **Mem0, Letta, Graphiti, Cognee, MemOS, Memobase as the core** | All are Python or TypeScript services, most want Postgres, Neo4j or Qdrant, and several default to a hosted product. A laptop CLI needs one binary and no daemon. Use their ideas, not their runtime |
| **OpenMemory** | The repo name now holds a different tool and the docs route is gone. Status is not verified. Do not put it on the critical path |
| **Basic Memory** | AGPL-3.0. Fine to read, risky to link into a shipped CLI |
| **TruffleHog** | AGPL-3.0, and its verification step calls secret providers. Both are wrong for a local-first tool that promises nothing leaves the laptop |
| **detect-secrets and scrubadub** | Last releases in 2024 and 2023. Their rule lists are still useful to read |
| **Presidio as an embedded dependency** | Right idea, wrong runtime for a Go CLI. Python plus spaCy models is hundreds of megabytes. Copy its entity list |
| **claude-mem as the memory layer** | Strong UX, but a large TypeScript hook stack and a hosted default. Take the hook placement, not the runtime |
| **Keyless cosign as the default** | Keyless signing needs Fulcio and Rekor. A developer on a plane cannot sign. Default to age or a key-based signature |
| **Cursor as a required source** | Closed format, split stores, no notice on change. Read it best effort and never let a rule depend on it alone |
| **Auto-approving extracted rules** | Paper 2611.23476 shows self-editing harnesses regress and bloat. Paper 2610.38521 shows a rule file can steer code without telling the developer. A human approves, and the pack names the source file |

### 7.3 What you must write yourself

- The extraction step. No open-source Go project turns session turns into cited, durable rules. The closest published loop is a paper, not a library.
- The redaction policy layer: typed placeholders, a deny list per repo, and a hard stop when a rule text still matches a detector.
- The approval and provenance model: who approved which text, against which commit, with which source hash.
- The reviewer adapter. Feeding a signed pack to an AI reviewer means writing the prompt, the merge-base read and the "this rule came from INC-31" citation.

### 7.4 Risks and open questions

- **Format drift.** Claude Code JSONL and Cursor's SQLite are undocumented. Budget one adapter fix per quarter and keep a fixture per format.
- **Rule quality.** Nothing in the sources proves that rules mined from logs help. Paper 2611.22017 suggests skills help less in deployment than in demos. Plan an A/B test with a control repo before you sell the claim.
- **Licence drift.** TruffleHog is AGPL today. Check the licence again before you add any dependency.
- **Star counts are not adoption.** Several repos here have very high stars and a small code surface. Read the commit history before you depend on one.
- **Not verified:** whether OpenMemory still ships, whether minisign has an official Go library, and whether any of these tools redacts a whole transcript without losing code context. The last one needs a test on your own corpus.

---

## 8. One-page answer

Build the reader on SpecStory, the secret pass on gitleaks, the PII pass on ferret-scan, the encryption on age, and the output on AGENTS.md. Store rules in SQLite with a citation and an approval. Keep every memory product out of the runtime. Trust is the differentiator: a cited rule that a human approved and a reviewer can use.
