# one-context production implementation plan

## Release target

one-context is a local, cross-platform context compiler for Windows, macOS, and
Linux. It watches registered repositories, compiles durable project state, and
lets coding tools load the same handoff without an MCP server.

The first public release is `v0.1.0-alpha.1`. It is a usable cross-platform
alpha, not a stability promise. `v1.0.0` is reserved for a demonstrated stable
artifact contract, installers, upgrades, observability, and release support.

## Product contract

The system owns one project-local state directory:

```text
<project>/.one-context/
  context.md       deterministic source of truth
  llm-context.md   optional model-written handoff
  project.json     structured evidence, schema, and fingerprint
```

`context.md` is generated from observable facts: Git state, changed paths,
bounded diffs, selected project anchors, current task, and handoff state.
`llm-context.md` is a derived summary. It must never replace the factual file,
change the evidence fingerprint, or be required for a refresh to succeed.

The product cannot read hidden Codex, Claude, Cursor, or IDE conversation
windows. A current task or next step persists once explicitly recorded, but it
cannot be inferred reliably from a conversation it does not own.

## Scope

In scope for `v0.1.0-alpha.1`:

- Windows, macOS, and Linux binaries for amd64 and arm64 where supported.
- One-command repository enrollment and background refresh.
- Filesystem events plus periodic Git reconciliation.
- Deterministic and optional LLM handoffs.
- Ollama, OpenAI-compatible, Anthropic, and Gemini providers.
- Safe Codex and Claude Code integration files.
- Clear health, repair, pause, resume, and uninstall paths.

Out of scope for the alpha:

- MCP as a core data path.
- Chat-history scraping or automatic private-session import.
- Cloud sync, team sharing, embeddings/RAG, and a desktop GUI.
- Automatic edits by an LLM.
- Marketplace package-manager distribution before release artifacts are proven.

## Architecture

Use one Go binary. It owns the terminal UI, watcher, scheduler, Git inspection,
state registry, artifact generation, provider HTTP clients, and OS service
adapters. Do not add Python unless a future model feature has a concrete,
measured dependency that cannot be handled in Go.

```text
filesystem events      periodic Git reconciliation
        |                         |
        +---- debounce + dedupe ---+
                                  |
                         evidence compiler
                    Git + anchors + task state
                                  |
                         deterministic context.md
                                  |
                       optional bounded LLM call
                                  |
                          derived llm-context.md
                                  |
                   Codex skill / Claude slash command
```

State rules:

- The global registry is user-scoped and versioned. It stores project paths,
  service state, provider choice, and model name, never API keys.
- API keys come only from environment variables or an OS-backed secret provider
  added in a later milestone.
- Project writes use atomic replace. Readers must always see a complete file.
- Fingerprints represent deterministic evidence only, including diff content.
- Input, output, changed-file count, diff size, scan duration, and concurrent
  work all have hard bounds.

## Provider contract

Provider configuration is explicit and local:

```text
/llm ollama [model]
/llm api <model> [base-url]
/llm claude <model> [base-url]
/llm gemini <model> [base-url]
/llm off
```

- Ollama is the default private option and must work offline.
- API providers are opt-in per user. The CLI states that repository excerpts
  will leave the machine before enabling one.
- Common credential-file paths and token-shaped values are excluded or redacted
  before a remote request. This is defense in depth, not a privacy guarantee.
- Every call has input caps, output caps, a timeout, cancellation, and a
  provider-specific error message.
- Provider failure writes a valid deterministic context and a visible warning;
  it never blocks the daemon.
- Provider adapters are tested with contract fixtures and an opt-in live smoke
  test that uses a user-provided key outside CI.

## Delivery milestones

### 0. Repository reset and alpha boundary

- Track all Go source, tests, modules, CI, release configuration, and docs.
- Archive or remove root MCP/Python artifacts, debug launchers, caches,
  distributions, backups, and stale local state. Preserve user data by moving
  it to an explicit archive before deletion.
- Add `LICENSE`, `SECURITY.md`, `CODE_OF_CONDUCT.md`, `CONTRIBUTING.md`,
  `CHANGELOG.md`, and an alpha support statement.
- Rename/document the product as a new non-MCP release line. Do not reuse
  `0.7.0` or present it as an upgrade path.

Acceptance: `git clone` followed by documented build and tests produces the
same CLI on a clean machine. `git status` contains no generated product output.

### 1. Artifact and state correctness

- Registry schema v2 includes a tested v1 forward migration. `project.json`
  remains backward-readable for v1 and is rewritten as v2 on the next compile;
  unknown future schemas fail safely with an explicit error.
- Persist the last processed Git base and retain the latest meaningful change
  summary after commits, not only dirty working-tree details.
- Preserve task, next-step, decision, and handoff fields until superseded.
- Detect non-Git folders deliberately, with a reduced file-based mode or a
  clear unsupported error.
- Add a `validate` command that checks artifacts, permissions, paths, and
  schema without changing state.

Acceptance: commits, renames, deletes, empty working trees, missed events,
restarts, and provider failures preserve a correct handoff with no duplicate
or partial artifact writes.

### 2. Daemon reliability and OS lifecycle

- Create one lifecycle interface with implementations for Windows Task
  Scheduler, macOS LaunchAgent, and Linux systemd user service.
- Add install, status, restart, uninstall, and repair commands. Startup setup
  is always explicit and reversible.
- Replace silent watcher failures with structured logs and visible project
  health states. Reconcile after overflow, permission failure, or wake-from-
  sleep.
- Add single-instance locking with stale-lock validation, graceful shutdown,
  bounded queues, crash restart policy, and exponential backoff for providers.
- Add a low-frequency reconciliation scan that cannot overlap with itself.

Acceptance: the service survives restart, sleep/wake, provider outage, a
deleted project folder, and a missed watch event without corrupting artifacts.

### 3. Production CLI experience

- Keep the command palette as the default UI: `/add`, `/status`, `/integrate`,
  `/llm`, `/pause`, `/resume`, `/repair`, `/logs`, `/uninstall`, `/help`.
- Add non-interactive equivalents with stable exit codes and JSON output for
  automation.
- Show project health, last successful compile, last meaningful change,
  provider state, artifact paths, queue state, and actionable errors.
- Respect `NO_COLOR`, provide plain output when piped, and avoid color as the
  only status signal.
- Add safe folder selection, validation, and confirmation before writing a tool
  integration or startup service.

Acceptance: a new user registers a project, enables a provider, installs one
assistant bridge, and verifies health without reading a separate guide.

### 4. Tool adapters and context continuity

- Codex adapter installs a small global skill that resolves the active
  workspace and reads `.one-context/context.md` plus optional
  `llm-context.md`.
- Claude Code adapter installs a project-local `/one-context` command without
  modifying existing `CLAUDE.md`.
- Add adapters only when their integration format is documented and tested.
- Provide an explicit, one-action way to record task/next-step/handoff state
  from the palette and supported assistants. Do not pretend this can happen
  from inaccessible private chats.
- Test existing project instruction files, paths with spaces, nested repos, and
  missing artifacts.

Acceptance: Codex and Claude Code both load the correct current project
handoff without duplicate context storage or instruction-file overwrite.

### 5. LLM quality, privacy, and cost controls

- Create a versioned evaluation corpus of representative diffs, commits,
  tasks, sensitive-looking source, no-change runs, and provider failures.
- Score factual coverage, unsupported claims, omission rate, output length,
  latency, and token cost against deterministic baseline artifacts.
- Add redaction rules for obvious secrets before an API request, with a clear
  statement that redaction is defense-in-depth rather than a guarantee.
- Add input-size preview. The global kill switch (`/llm off`), daily request
  budget (`/llm limit <n>`), and per-project LLM allow/deny control
  (`config <project> --llm on|off`) are implemented.
- Record provider/model/version, duration, input size, output size, and error
  class without logging source excerpts or credentials.

Acceptance: no provider becomes the default until it passes a fixed factuality
threshold and all failure cases leave deterministic output valid.

### 6. Cross-platform packaging and release engineering

- Build reproducible archives for Windows, macOS, and Linux with checksums.
- Sign release artifacts and publish provenance/SBOM data.
- Test Windows x64/arm64, macOS x64/arm64, and Linux x64/arm64 where runners
  are available. Do not claim an architecture that was not tested.
- Add clean-install smoke tests for every archive: install, add a sample repo,
  trigger an edit, restart service, inspect both artifacts, uninstall.
- Publish GitHub release archives first. Add winget, Homebrew, and Linux package
  distribution only after their installers and update paths are exercised.

Acceptance: an external tester installs a signed binary, completes the primary
flow, upgrades once, and uninstalls without leftover service processes.

### 7. Public alpha release gate

- Require a clean working tree and all product source tracked before tagging.
- Run hosted CI on the exact release commit: formatting, unit, integration,
  race, static analysis, dependency audit, secret scan, artifact smoke tests,
  and cross-platform matrix.
- Require manual acceptance on each OS for CLI readability, daemon lifecycle,
  tool adapter behavior, and provider-off fallback.
- Publish privacy, data-handling, retention, supported-platform, and known
  limitations documents with the release.
- Tag only after review of release notes, checksums, SBOM/provenance, and the
  rollback/uninstall procedure.

Acceptance: `v0.1.0-alpha.1` is a traceable, signed, reproducible release with
known limitations stated publicly.

## Test strategy

- Unit: parsing, fingerprinting, state migrations, path validation, artifact
  rendering, provider response parsing, redaction, and CLI exit codes.
- Integration: real Git repositories covering commits, dirty changes, renames,
  deletes, ignored paths, nested directories, and project paths with spaces.
- Daemon: file events, event overflow, debounce, reconciliation, service
  restart, stale lock, cancellation, and concurrent project updates.
- Provider: local mock contracts in CI; opt-in live smoke tests outside CI.
- End-to-end: clean install on every target OS and assistant adapter fixtures.
- Quality: LLM evaluation corpus must compare each model against deterministic
  evidence and reject unsupported factual claims.

## Non-negotiable release gates

Do not tag or market a production release until all are true:

1. Source, tests, release configuration, and documentation are tracked and a
   clean clone builds on supported platforms.
2. Hosted CI passes on the release commit, including race tests and artifact
   smoke tests.
3. The daemon has logs, repair/uninstall procedures, and explicit recovery from
   watcher/provider failure.
4. LLM output is evaluated, optional, bounded, and unable to corrupt factual
   context.
5. API keys never enter the repository, registry, generated artifacts, logs,
   crash bundles, or support exports.
6. Signed artifacts, checksums, SBOM/provenance, release notes, privacy policy,
   and known limitations are published.

## Version policy

- `v0.1.x-alpha`: breaking changes are allowed with migration notes.
- `v0.2.x-beta`: installer and artifact formats stabilize; upgrades are tested.
- `v1.0.0`: only after the release gates above pass over multiple public beta
  releases and the context artifact contract is stable.
