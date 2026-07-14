# Contributing to reqmin

Issues, discussions and pull requests are all welcome.

## Getting started

You need Go ≥1.22; nothing else — the project has zero runtime
dependencies and the test suite runs fully offline.

```bash
git clone https://github.com/JaydenCJ/reqmin && cd reqmin
go build ./...
go test ./...
bash scripts/smoke.sh
```

`scripts/smoke.sh` builds the binary and the loopback demo API, then walks
the real workflow — dry-run enumeration, minimizing a fat browser capture,
raw-HTTP stdin input, JSON reports, and the exit-code contract; it must
finish by printing `SMOKE OK`.

## Before you open a pull request

1. `gofmt -l .` reports nothing (formatting is enforced).
2. `go vet ./...` passes with no findings.
3. `go test ./...` passes (92 deterministic tests, no network).
4. `bash scripts/smoke.sh` prints `SMOKE OK`.
5. Add tests for behavior changes; keep logic in pure, unit-testable
   modules (the parser, the item plan, and ddmin never touch the network —
   only `runner` does I/O, and `cli` wires everything together).

## Ground rules

- Keep dependencies at zero; adding one needs strong justification in
  the PR.
- No network calls at startup, no telemetry; the only outbound traffic is
  the probes the user explicitly asked for, against the target they named.
- Candidate materialization must stay lossless: surviving headers, query
  pairs, form fields, and JSON members are never re-encoded or reordered.
  A diff between the original and the minimized request may only ever
  show deletions.
- Every probe must go through `internal/runner` so the memo cache and the
  request budget stay authoritative.
- Code comments and doc comments are written in English.

## Reporting bugs

Include the output of `reqmin --version`, the exact input (a redacted curl
command or raw request is fine), the `--expect-*` flags you used, and the
stderr report. For wrong minimizations, `--verbose` output plus a
description of what the server actually requires is what makes the case
reproducible.

## Security

Please do not open public issues for security problems; use GitHub's
private vulnerability reporting on this repository instead.
