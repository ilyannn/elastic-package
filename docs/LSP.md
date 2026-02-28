# Plan: `elastic-package lsp` (Future-Complete Roadmap)

## Goal

Build an LSP server that starts with reliable diagnostics and evolves into a full editing experience for Elastic integrations:
- Fast, accurate validation feedback while editing.
- Intelligent completion/hover/navigation for package-spec, ECS, and integration structure.
- Code actions and refactors for common package authoring tasks.
- Stable behavior across large repos and long IDE sessions.

## What We Learned From Existing LSPs (Go-first)

### `gopls` patterns to adopt
- Long-running server with in-memory workspace state and cache invalidation.
- Capabilities rolled out incrementally while keeping protocol compatibility.
- Multiple diagnostic lanes (fast local + slower workspace) and explicit latency goals.
- Clear separation between transport, protocol, and language intelligence.

### `terraform-ls` patterns to adopt
- Public feature matrix that marks implemented vs planned LSP methods.
- Strong workspace lifecycle support (`didOpen`, `didChange`, `didClose`, watched files).
- Incremental delivery model: methods can be "implemented but maturing".

### `helm-ls` patterns to adapt
- Domain LSP + external YAML/schema intelligence is practical and useful.
- Configurability is critical (toggle noisy diagnostics, tune behavior per workspace).
- Mixed diagnostic sources need dedupe and source labeling.

### `glsp` (framework) takeaway
- Framework can reduce protocol boilerplate, but increases dependency/upgrade surface.
- For this repo, start dependency-light; keep interfaces so transport can be swapped later.

## Architecture Options (Pros / Contras)

### Option A: Hand-rolled JSON-RPC + LSP subset (stdlib only)
**Pros**
- Minimal dependencies and full control.
- Easy to keep binary behavior deterministic.
- Fits current codebase style.

**Contras**
- More protocol edge-cases to own (framing, cancellation, lifecycle).
- Slower to add broad method coverage.
- Higher long-term maintenance burden without strict abstraction.

### Option B: Use a Go LSP framework (for example GLSP)
**Pros**
- Faster implementation of protocol plumbing.
- Better out-of-box method scaffolding.

**Contras**
- New dependency with its own release cadence.
- Harder to control low-level behavior and compatibility quirks.
- May not match repo preference for conservative dependency growth.

### Option C: Hybrid language intelligence (internal engine + external yamlls/schema tools)
**Pros**
- Faster path to rich YAML completion/hover.
- Reuses mature schema tooling.

**Contras**
- More moving parts and process orchestration.
- Diagnostics can be noisy or conflicting.
- Debugging and reproducibility become harder.

## Decision

Start with **Option A** for protocol transport and server lifecycle, but design clean interfaces so we can later adopt parts of **Option C** (schema/semantic enrichment) without rewriting the server loop.

## Non-Goals (Phase 1)

- Full semantic rename/refactor across packages.
- Advanced workspace-wide symbol indexing.
- Notebook APIs and uncommon LSP extensions.

## LSP Capability Roadmap

### Phase 1: Solid Diagnostics MVP
Methods:
- `initialize`, `initialized`, `shutdown`, `exit`
- `$/cancelRequest` (acknowledge and discard; prevents unknown-method noise from aggressive clients)
- `textDocument/didOpen`, `textDocument/didSave`, `textDocument/didClose`
- `textDocument/publishDiagnostics`

Behavior:
- Resolve package root from edited file using `packages.FindPackageRootFrom`.
- Run `validation.ValidateAndFilterFromPath`, which returns `(processedErrors, skippedErrors)` — both typed as `error`. Type-assert the first return value to `specerrors.ValidationErrors` to iterate individual errors. If the assertion fails (plain `error`, not `ValidationErrors`), treat the whole thing as a single package-level diagnostic.
- Extract per-file paths from error messages using two regex patterns:
  - `file "([^"]+)"` — the dominant pattern in `folder_spec.go` and semantic validators. Captures a **relative** path within the package root (e.g. `data_stream/access/fields/base-fields.yml`). Join with package root to produce an absolute path for the URI.
  - `folder \[([^\]]+)\]` — folder-level errors (size limits, missing items). Map to the folder's nearest manifest or fall back to package `manifest.yml`.
  - Errors matching neither pattern go to package `manifest.yml` as a catch-all bucket.
- All diagnostics carry `"source": "elastic-package"`.
- Publish per-file diagnostics and clear stale diagnostics for files that no longer have errors.
- On `textDocument/didClose`: clear diagnostics for that URI, remove it from the tracked set. If no more open files reference a given package root, stop tracking that root.
- Multi-package workspace: track open-file-to-package-root associations. Validating one package never affects diagnostics for another.

### Phase 2: Live Editing Correctness
Methods:
- `textDocument/didChange` (full-sync first)
- `workspace/didChangeWatchedFiles` (optional by client support)

Behavior:
- Maintain in-memory document store for open files.
- Debounced diagnostics on change (for example 300-800ms).
- Fallback to save-triggered validation when in-memory validation is unavailable.

### Phase 3: Authoring Intelligence
Methods:
- `textDocument/hover`
- `textDocument/completion`
- `textDocument/documentSymbol`
- `textDocument/codeAction` (quick fixes)

Behavior:
- Hover/completion from package-spec schema metadata.
- Completion for known keys/enums in `manifest.yml`, data stream manifests, and policy templates.
- Quick fixes for high-confidence lint failures (missing required keys, known invalid enum values).

### Phase 4: Navigation and Refactors
Methods:
- `textDocument/definition`
- `textDocument/references`
- `textDocument/rename` (guarded rollout)

Behavior:
- Navigate between references in integration assets (ingest pipelines, templates, dataset references).
- Start rename on constrained symbols only; expand after telemetry and test confidence.

## Performance and Reliability Targets

- Open/save diagnostics visible in under 1s for common packages.
- Did-change diagnostics visible in under 300ms for small edits once Phase 2 lands.
- No stdout contamination from non-LSP output.
- Server remains healthy across multi-hour editor sessions.

## Integration Safety Requirements

- `lsp` command must bypass update checks and any startup side effects that can block or write unrelated output.
- Implement bypass in `checkVersionUpdate(cmd, args)` by early-returning when the invoked command is `lsp` (rather than overriding `PersistentPreRunE` in `cmd/lsp.go`), so `lsp` still inherits other root pre-run behavior consistently.
- All logs must go to stderr only. The `logger` package uses Go's `log.Print` which defaults to `os.Stderr`, but this is implicit. The `lsp` command action must explicitly call `log.SetOutput(os.Stderr)` before starting the server as a safety guard against any future code or dependency that redirects the default logger.
- Capabilities must match implemented methods exactly (no false advertising).

## Internal Package Layout

### Phase 1 files

#### `internal/lsp/jsonrpc.go`
- Message framing (`Content-Length`) + marshal/unmarshal.
- `sendNotification`, `sendResponse`, `sendErrorResponse`.

#### `internal/lsp/protocol.go`
- Minimal protocol types for Phase 1 methods.
- URI/path helpers (relative-to-absolute joining with package root) and position/range helpers.

#### `internal/lsp/server.go`
- Main event loop and lifecycle state machine (`initialized`, `shutdown` flags).
- Capability negotiation (advertise only implemented methods).
- `$/cancelRequest` handler (acknowledge, no-op).
- Graceful shutdown behavior.

#### `internal/lsp/diagnostics.go`
- Validation execution pipeline.
- Error-to-diagnostic mapping: type-assert `specerrors.ValidationErrors`, iterate, extract file paths via regex, join relative paths with package root.
- Two regex extractors: `file "([^"]+)"` and `folder \[([^\]]+)\]`.
- Fallback: unmatched errors → package `manifest.yml`.
- Stale diagnostics tracking: set of previously-published URIs per package root; send empty `[]` for URIs that no longer have errors.

#### `internal/lsp/workspace.go` (thin in Phase 1)
- Open-file set and URI → package-root associations.
- On `didClose`: remove URI, clear diagnostics, garbage-collect package roots with no remaining open files.

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
- Diagnostics mapping, stale clear behavior, and package-root-not-found handling.

### Integration tests
- Start server over stdio and replay real framed LSP transcripts.
- Edit/save scenarios in fixtures under `test/packages/...`.
- Multiple open files from same package and from different packages.

### Compatibility tests
- Capability assertions against client expectations.
- Regression fixtures for package-spec error message variants.

## Feature Matrix (Living Document)

Status key: **impl** = implemented, **exp** = experimental, **plan** = planned, **n/a** = not applicable.

### Requests

| LSP method | Status | Phase | Note |
| :--- | :---: | :---: | :--- |
| `initialize` | plan | 1 | |
| `shutdown` | plan | 1 | |
| `textDocument/completion` | plan | 3 | |
| `completionItem/resolve` | plan | 3 | |
| `textDocument/hover` | plan | 3 | |
| `textDocument/documentSymbol` | plan | 3 | |
| `textDocument/codeAction` | plan | 3 | Quick fixes |
| `textDocument/definition` | plan | 4 | |
| `textDocument/references` | plan | 4 | |
| `textDocument/rename` | plan | 4 | Guarded rollout |
| `textDocument/formatting` | n/a | — | Use `elastic-package format` externally |

### Notifications

| LSP method | Status | Phase | Note |
| :--- | :---: | :---: | :--- |
| `initialized` | plan | 1 | |
| `exit` | plan | 1 | |
| `$/cancelRequest` | plan | 1 | Acknowledge, no-op |
| `textDocument/didOpen` | plan | 1 | Triggers validation |
| `textDocument/didSave` | plan | 1 | Triggers validation |
| `textDocument/didClose` | plan | 1 | Clears diagnostics, GC package root |
| `textDocument/publishDiagnostics` | plan | 1 | Server → client |
| `textDocument/didChange` | plan | 2 | Full-sync first |
| `workspace/didChangeWatchedFiles` | plan | 2 | Optional by client |

Update statuses as implementation progresses.

## Initial Implementation Checklist (Phase 1)

1. Add `cmd/lsp.go` and root command wiring.
2. Build `internal/lsp/jsonrpc.go`, `protocol.go`, `server.go`, `diagnostics.go`.
3. Implement phase-1 methods and capability response.
4. Add strict stdout/stderr safety guards.
5. Add unit tests + transcript-based integration tests.
6. Validate in VS Code and at least one additional LSP client.

## Verification

1. `just build`
2. `just test-pkg ./internal/lsp/...`
3. Replay framed initialize/shutdown transcript against `elastic-package lsp`.
4. Manual IDE smoke test with a broken fixture package; confirm diagnostics publish and clear.
