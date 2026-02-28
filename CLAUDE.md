# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

`elastic-package` is a Go CLI tool for developing Elastic Integration packages. It handles linting, formatting, testing, building, and deploying packages to the Elastic Stack. Built with Cobra (`spf13/cobra`), validated against `elastic/package-spec`.

## Build & Dev Commands

```bash
just build          # build binary with version ldflags
just check          # full static check: build, format, lint, licenser, gomod, update, git-clean
just install        # build and install to ~/.local/bin
just test           # run unit tests (gotestsum, -count=1 to skip cache)
just format         # goimports
just lint           # staticcheck
```

Run a single test or package:
```bash
just test-pkg ./internal/packages/...
just test-pkg -run TestSpecificName ./internal/kibana/...
```

Integration test with a test package (requires Docker stack):
```bash
just run stack up -v -d
just test-integration ./test/packages/parallel/apache
```

## Architecture

**Entry point**: `main.go` -> `cmd/root.go` (Cobra root command)

**Internal package import hierarchy** (must remain acyclic):
```
internal/packages         ← base types for package loading/manipulation
    ↑
internal/kibana           ← Kibana API client (imports packages)
    ↑
internal/resources        ← resource lifecycle CRUD (imports kibana, packages)
    ↑
internal/testrunner       ← test execution framework (imports resources, kibana, packages)
```

**Key internal packages**:
- `internal/builder/` - package build logic
- `internal/stack/` - stack management (providers: compose, environment, serverless)
- `internal/servicedeployer/` - service deployment for tests
- `internal/fields/` - field validation
- `internal/fleetpkg/` - Fleet objects-based API (not deprecated arrays-based)
- `internal/compose/` - Docker Compose integration

**Test packages** live in `test/packages/` (parallel, with-kind, other, false_positives, etc.). Prefer using real fixtures from `test/packages/` or `testdata/` dirs over inline YAML.

## Code Conventions

- Keep exported surface small; unexport functions only used within the same package.
- Wrap errors with context: `fmt.Errorf("...: %w", err)`.
- No trailing spaces or trailing newlines at end of files.
- Place functions in the package that owns the types they operate on:
  - `packages.*` types -> `internal/packages`
  - `kibana.*` types -> `internal/kibana`
  - Resource lifecycle -> `internal/resources`
  - Test runner logic -> `internal/testrunner`
- Disk I/O at call site, not inside pure builder/helper functions.

## Fleet Package Policy API

Uses objects-based Fleet API (`PackagePolicy`), not deprecated arrays-based API.
- Input key: `"{policyTemplate.Name}-{input.Type}"`
- Stream key: `datasetKey(pkgName, ds)` — uses `ds.Dataset` when set, otherwise `"{pkgName}.{ds.Name}"`
- Always send `{enabled: false}` for sibling data streams sharing the same input type
- Objects-based API expects raw values, not `{"type": ..., "value": ...}` wrappers

## CI

Buildkite-based (`.buildkite/pipeline.yml`). PR comment `test integrations` triggers cross-repo testing against elastic/integrations. Comment `test serverless` triggers serverless testing.

## Release

GoReleaser triggered by git tags (semver). Version info embedded via ldflags into `internal/version`.
