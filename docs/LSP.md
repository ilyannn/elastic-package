# Plan: `elastic-package lsp` (Future-Complete Roadmap)

## Goal

Build an LSP server that starts with reliable diagnostics and evolves into a
full editing experience for Elastic integrations.

- Fast, accurate validation feedback while editing.
- Intelligent completion/hover/navigation for package-spec, ECS, and integration
  structure.
- Code actions and refactors for common package authoring tasks.
- Stable behavior across large repos and long IDE sessions.

## What We Learned From Existing LSPs (Go-first)

### `gopls` patterns to adopt

- Long-running server with in-memory workspace state and cache invalidation.
- Capabilities rolled out incrementally while keeping protocol compatibility.
- Multiple diagnostic lanes (fast local + slower workspace) and explicit latency
  goals.
- Clear separation between transport, protocol, and language intelligence.

### `terraform-ls` patterns to adopt

- Public feature matrix that marks implemented vs planned LSP methods.
- Strong workspace lifecycle support (`didOpen`, `didChange`, `didClose`,
  watched files).
- Incremental delivery model: methods can be "implemented but maturing".

### `helm-ls` patterns to adapt

- Domain LSP + external YAML/schema intelligence is practical and useful.
- Configurability is critical (toggle noisy diagnostics, tune behavior per
  workspace).
- Mixed diagnostic sources need dedupe and source labeling.

### `glsp` (framework) takeaway

- Framework can reduce protocol boilerplate, but increases dependency/upgrade
  surface.
- For this repo, start dependency-light; keep interfaces so transport can be
  swapped later.

## Architecture Options (Pros / Contras)

### Option A: Hand-rolled JSON-RPC + LSP subset (stdlib only)

#### Option A Pros

- Minimal dependencies and full control.
- Easy to keep binary behavior deterministic.
- Fits current codebase style.

#### Option A Contras

- More protocol edge-cases to own (framing, cancellation, lifecycle).
- Slower to add broad method coverage.
- Higher long-term maintenance burden without strict abstraction.

### Option B: Use a Go LSP framework (for example GLSP)

#### Option B Pros

- Faster implementation of protocol plumbing.
- Better out-of-box method scaffolding.

#### Option B Contras

- New dependency with its own release cadence.
- Harder to control low-level behavior and compatibility quirks.
- May not match repo preference for conservative dependency growth.

### Option C: Hybrid intelligence (internal + external schema tools)

#### Option C Pros

- Faster path to rich YAML completion/hover.
- Reuses mature schema tooling.

#### Option C Contras

- More moving parts and process orchestration.
- Diagnostics can be noisy or conflicting.
- Debugging and reproducibility become harder.

## Decision

Start with **Option A** for protocol transport and server lifecycle, but design
clean interfaces so we can later adopt parts of **Option C**
(schema/semantic enrichment) without rewriting the server loop.

## Core Architecture Pattern (Industry-aligned)

- `ProtocolLoop`: JSON-RPC read/write, request/notification dispatch,
  capability negotiation.
- `Scheduler`: job queue with latest-wins coalescing per workspace root and URI.
  Cancel stale jobs before they publish.
- `AnalysisStore`: in-memory state per workspace root (open docs, package-root
  map, derived indexes and caches).
- `DiagnosticsPublisher`: versioned publish pipeline that sends only newest
  diagnostics per URI and clears stale diagnostics.
- `WorkspaceManager`: multi-root lifecycle owner
  (`initialize.workspaceFolders`, `workspace/didChangeWorkspaceFolders`) with
  root-level state garbage collection.
- Keep interfaces explicit so validation backend and protocol transport can
  evolve independently.

## Non-Goals (Phase 1)

- Full semantic rename/refactor across packages.
- Advanced workspace-wide symbol indexing.
- Notebook APIs and uncommon LSP extensions.

## Workspace Model (Multi-root First-class)

- Multi-root is supported by default when the client advertises
  `workspaceFolders`.
- Keep state compartmentalized by workspace root to avoid cross-root pollution.
- If an opened URI is outside known roots but resolves to a package, create
  lazy state for that root.
- On workspace-folder removal: cancel jobs for that root, clear diagnostics for
  all tracked URIs in that root, then drop caches.
- If client does not support `workspaceFolders`, fall back to single-root mode
  from `initialize.rootUri` or `rootPath`.

## Operational Guarantees

### Resource budgets and eviction

- Use bounded in-memory state to keep long-running sessions healthy.
- Phase 1 enforced limits:
  - Max active workspace roots: 16 (new roots beyond the limit are rejected).
  - Max tracked open docs per root: 500 (`didOpen` tracking is capped per root).
  - Scheduler queue size: 64 (bounded queue with coalescing and backpressure).
- Behavior on pressure:
  - If root/doc limits are reached, the server logs warnings and rejects excess
    tracked state.
  - If scheduler queue is full, enqueue attempts are dropped with structured
    backpressure logs.
- Phase 2+ planned hardening:
  - Soft process memory budget for derived caches.
  - LRU eviction of inactive per-root derived caches.

### Failure isolation and recovery

- Never let a single request, workspace root, or validator panic crash the
  server process.
- Recover panics at both protocol-dispatch and scheduler-worker boundaries.
- Convert recovered panics into structured error logs with request/root context.
- On workspace-root removal, cancel pending/running jobs tied to that root.
- Keep serving unaffected roots while one root is degraded.
- Phase 2+ planned hardening:
  - Root-scoped retry/backoff for repeatedly failing jobs.

### Deterministic root precedence

- When workspace roots overlap (for example `/repo` and `/repo/sub`), resolve
  URIs by longest-prefix root match.
- Package-root discovery must stay constrained within the chosen workspace root.

## LSP Capability Roadmap

### Phase 1: Solid Diagnostics MVP

Methods:

- `initialize`, `initialized`, `shutdown`, `exit`
- `$/cancelRequest` (acknowledge and discard; prevents unknown-method noise from
  aggressive clients)
- `textDocument/didOpen`, `textDocument/didSave`, `textDocument/didClose`
- `textDocument/publishDiagnostics`
- `workspace/didChangeWorkspaceFolders` (when client supports multi-root)

Behavior:

- Resolve package root from edited file using `packages.FindPackageRootFrom`.
- Run `validation.ValidateAndFilterFromPath`, which returns
  `(processedErrors, skippedErrors)` — both typed as `error`.
  Type-assert the first return value to `specerrors.ValidationErrors` to iterate
  individual errors. If the assertion fails (plain `error`, not
  `ValidationErrors`), treat the whole thing as a single package-level
  diagnostic.
- Extract per-file paths from error messages using two regex patterns:
  - `file "([^"]+)"`: dominant pattern in `folder_spec.go` and semantic
    validators. Captures a **relative** path within the package root (for
    example `data_stream/access/fields/base-fields.yml`). Join with package root
    to produce an absolute path for the URI.
  - `folder \[([^\]]+)\]`: folder-level errors (size limits, missing items).
    Map to the folder's nearest manifest or fall back to package `manifest.yml`.
  - Errors matching neither pattern go to package `manifest.yml` as a catch-all.
- All diagnostics carry `"source": "elastic-package"`.
- Publish per-file diagnostics and clear stale diagnostics for files that no
  longer have errors.
- On `textDocument/didClose`: clear diagnostics for that URI, remove it from the
  tracked set. If no more open files reference a given package root, stop
  tracking that root.
- Multi-package workspace: track open-file-to-package-root associations.
  Validating one package never affects diagnostics for another.
- Root resolution order: URI -> workspace folder (if available) -> package root
  discovery. Never publish diagnostics across workspace-root boundaries.

### Phase 2: Live Editing Correctness

Methods:

- `textDocument/didChange` (full-sync first)
- `workspace/didChangeWatchedFiles` (optional by client support)
- `workspace/didChangeConfiguration`

Behavior:

- Maintain in-memory document store for open files.
- Debounced diagnostics on change (for example 300-800ms).
- Fallback to save-triggered validation when in-memory validation is unavailable.
- Apply workspace setting updates without restart (for example diagnostics
  debounce, feature gates, and log verbosity).

### Phase 3: Authoring Intelligence

Methods:

- `textDocument/hover`
- `textDocument/completion`
- `textDocument/documentSymbol`
- `textDocument/codeAction` (quick fixes)

Behavior:

- Hover/completion from package-spec schema metadata.
- Completion for known keys/enums in `manifest.yml`, data stream manifests, and
  policy templates.
- Quick fixes for high-confidence lint failures (missing required keys, known
  invalid enum values).

### Phase 4: Navigation and Refactors

Methods:

- `textDocument/definition`
- `textDocument/references`
- `textDocument/rename` (guarded rollout)

Behavior:

- Navigate between references in integration assets (ingest pipelines,
  templates, dataset references).
- Start rename on constrained symbols only; expand after telemetry and test confidence.

## Performance and Reliability Targets

- Open/save diagnostics visible in under 1s for common packages.
- Did-change diagnostics visible in under 300ms for small edits once Phase 2 lands.
- No stdout contamination from non-LSP output.
- Server remains healthy across multi-hour editor sessions.

## Observability and Debug Logging

- Emit debug logs to stderr for the initial rollout (we will need them to
  harden behavior across editors and repos).
- Use structured log fields at minimum: timestamp, level, method, request ID,
  URI, workspace root, package root, duration_ms, diagnostics_count,
  cancelled/coalesced flags.
- Log scheduler transitions: queued, coalesced, cancelled, started, finished,
  publish-skipped-stale.
- Log budget pressure and eviction events (cache-evicted, queue-backpressure,
  dropped-superseded).
- Log panic recovery events with component and root context.
- Never log full document contents; redact sensitive values when logging paths
  or settings payloads.
- Support log-level control via env/config (for example
  `ELASTIC_PACKAGE_LSP_LOG_LEVEL=debug|info|warn|error`) once
  `workspace/didChangeConfiguration` lands.

## Integration Safety Requirements

- `lsp` command must bypass update checks and any startup side effects that can
  block or write unrelated output.
- Implement bypass in `checkVersionUpdate(cmd, args)` by early-returning when the
  invoked command is `lsp` (rather than overriding `PersistentPreRunE` in
  `cmd/lsp.go`), so `lsp` still inherits other root pre-run behavior
  consistently.
- All logs (including debug logs) must go to stderr only. The `logger` package
  uses Go's `log.Print`, which defaults to `os.Stderr`, but this is implicit.
  The `lsp` command action must explicitly call `log.SetOutput(os.Stderr)`
  before starting the server as a safety guard against any future code or
  dependency that redirects the default logger.
- Capabilities must match implemented methods exactly (no false advertising).

## Internal Package Layout

### Phase 1 files

#### `internal/lsp/jsonrpc.go`

- Message framing (`Content-Length`) + marshal/unmarshal.
- `sendNotification`, `sendResponse`, `sendErrorResponse`.

#### `internal/lsp/protocol.go`

- Minimal protocol types for Phase 1 methods.
- URI/path helpers (relative-to-absolute joining with package root) and
  position/range helpers.

#### `internal/lsp/server.go`

- Main event loop and lifecycle state machine (`initialized`, `shutdown`
  flags).
- Capability negotiation (advertise only implemented methods).
- `$/cancelRequest` handler (acknowledge, no-op).
- Route validation jobs through scheduler (do not validate inline in transport
  handlers).
- Panic recovery at dispatch boundary; failed requests are logged and isolated.
- Graceful shutdown behavior.

#### `internal/lsp/diagnostics.go`

- Validation execution pipeline.
- Error-to-diagnostic mapping: type-assert `specerrors.ValidationErrors`,
  iterate, extract file paths via regex, join relative paths with package root.
- Two regex extractors: `file "([^"]+)"` and `folder \[([^\]]+)\]`.
- Fallback: unmatched errors → package `manifest.yml`.
- Stale diagnostics tracking: set of previously-published URIs per package
  root; send empty `[]` for URIs that no longer have errors.

#### `internal/lsp/scheduler.go`

- Latest-wins coalescing queue keyed by `(workspaceRoot, packageRoot, uri)`.
- Cancellation tokens for superseded jobs.
- Publish guard that drops stale job results by version.
- Bounded queue with backpressure and coalescing-before-enqueue.
- Panic recovery in worker execution path and root-scoped retry/backoff.

#### `internal/lsp/logging.go`

- Structured debug log helpers used across protocol loop, scheduler, and
  diagnostics.
- Stable log schema for test assertions and incident debugging.

#### `internal/lsp/workspace.go` (thin in Phase 1)

- Workspace-root registry and URI → package-root associations.
- Handles `workspace/didChangeWorkspaceFolders` add/remove events.
- On `didClose`: remove URI, clear diagnostics, garbage-collect package roots
  with no remaining open files.

#### `cmd/lsp.go`

- Cobra command wiring (`cobraext.ContextGlobal`) to run LSP server.
- Explicit `log.SetOutput(os.Stderr)` before server start.

#### `cmd/root.go`

- Register `setupLspCommand()`.
- Guard in `checkVersionUpdate` to skip when command is `lsp`.

### Phase 2+ files (extend existing)

#### `internal/lsp/workspace.go` (grows)

- In-memory document store for `didChange` buffers.
- Debounce scheduler for change-triggered validation.
- Per-root configuration snapshots from `workspace/didChangeConfiguration`.

#### `internal/lsp/logging.go` (grows)

- Dynamic log-level updates from config/env without process restart.

#### `internal/lsp/completion.go` (Phase 3)

- Schema-driven completion and hover providers.

#### `internal/lsp/navigation.go` (Phase 4)

- Definition/references resolvers for cross-file integration assets.

## Testing Strategy

### Unit tests

- JSON-RPC framing (valid/invalid headers, partial reads, bad payloads).
- URI/path normalization across OS path styles.
- Lifecycle (`initialize` -> requests -> `shutdown` -> `exit`).
- Unknown method handling (`method not found`).
- Scheduler behavior: coalescing, cancellation, and stale publish suppression.
- Scheduler backpressure and bounded-queue behavior under bursty input.
- Cache eviction policy (LRU of inactive derived caches) under budget pressure.
- Panic recovery at dispatch and worker boundaries.
- Diagnostics mapping, stale clear behavior, and package-root-not-found handling.
- Log schema snapshots for key events (open/save/publish/cancel).

### Integration tests

- Start server over stdio and replay real framed LSP transcripts.
- Edit/save scenarios in fixtures under `test/packages/...`.
- Multiple open files from same package and from different packages.
- Multi-root scenarios: workspace-folder add/remove, per-root cache isolation,
  and diagnostics clearing on root removal.
- Overlapping-root scenario (`/repo` + `/repo/sub`) validates longest-prefix
  root resolution.
- Degraded-root scenario validates unaffected roots continue to serve requests.

### Compatibility tests

- Capability assertions against client expectations.
- Regression fixtures for package-spec error message variants.
- Workspace capability checks (`workspaceFolders` support and
  `workspace/didChangeConfiguration` behavior).

## Feature Matrix (Living Document)

Status key:

- **impl**: implemented
- **exp**: experimental
- **plan**: planned
- **n/a**: not applicable

### Requests

- `initialize`: impl (Phase 1)
- `shutdown`: impl (Phase 1)
- `textDocument/completion`: plan (Phase 3)
- `completionItem/resolve`: plan (Phase 3)
- `textDocument/hover`: plan (Phase 3)
- `textDocument/documentSymbol`: plan (Phase 3)
- `textDocument/codeAction`: plan (Phase 3, quick fixes)
- `textDocument/definition`: plan (Phase 4)
- `textDocument/references`: plan (Phase 4)
- `textDocument/rename`: plan (Phase 4, guarded rollout)
- `textDocument/formatting`: n/a (use `elastic-package format` externally)

### Notifications

- `initialized`: impl (Phase 1)
- `exit`: impl (Phase 1)
- `$/cancelRequest`: impl (Phase 1, acknowledge no-op)
- `textDocument/didOpen`: impl (Phase 1, triggers validation)
- `textDocument/didSave`: impl (Phase 1, triggers validation)
- `textDocument/didClose`: impl (Phase 1, clears diagnostics and GC package root)
- `textDocument/publishDiagnostics`: impl (Phase 1, server → client)
- `workspace/didChangeWorkspaceFolders`: impl (Phase 1, multi-root lifecycle)
- `textDocument/didChange`: plan (Phase 2, full-sync first)
- `workspace/didChangeWatchedFiles`: plan (Phase 2, optional by client)
- `workspace/didChangeConfiguration`: plan (Phase 2, live settings updates)

Update statuses as implementation progresses.

## Initial Implementation Checklist (Phase 1)

1. Add `cmd/lsp.go` and root command wiring.
2. Build `internal/lsp/jsonrpc.go`, `protocol.go`, `server.go`,
   `workspace.go`, `scheduler.go`, `diagnostics.go`, and `logging.go`.
3. Implement phase-1 methods and capability response, including
   `workspace/didChangeWorkspaceFolders`.
4. Add strict stdout/stderr safety guards and structured debug logs.
5. Add unit tests + transcript-based integration tests, including multi-root
   and scheduler cancellation cases.
6. Implement bounded queue, cache-budget eviction, and panic-recovery guards.
7. Validate in VS Code and at least one additional LSP client.

## Verification

1. `just build`
2. `just test-pkg ./internal/lsp/...`
3. Replay framed initialize/shutdown transcript against `elastic-package lsp`.
4. Manual IDE smoke test with a broken fixture package; confirm diagnostics
   publish and clear.
5. Multi-root smoke test: open files from two workspace folders, validate
   isolation, then remove one folder and verify diagnostics clear for that root.
6. Debug-log smoke test: confirm structured stderr logs include method, request
   ID, root context, duration, and cancellation/coalescing markers.
7. Budget-pressure smoke test: generate bursty changes and confirm queue
   backpressure + superseded-job dropping behave as expected.
8. Failure-isolation smoke test: inject a failing validator path and confirm
   other workspace roots continue serving requests.
