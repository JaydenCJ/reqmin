# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-07-13

### Added

- ddmin delta-debugging engine over abstract item sets: 1-minimal results,
  memoized probes, an early empty-set check, and graceful budget
  exhaustion that returns the best reduction found so far.
- Request decomposition into removable atoms: individual headers, query
  parameters, cookies (split out of `Cookie` headers and rebuilt in
  place), `application/x-www-form-urlencoded` fields, nested JSON object
  keys (order- and literal-preserving via an ordered JSON tree), and
  whole opaque bodies.
- Lossless materialization: surviving query/form pairs keep their exact
  original percent-encoding, headers keep order and duplicates, and
  `Content-Length` is always recomputed.
- "Copy as cURL" input: a shell-grade tokenizer (single/double/`$'…'`
  quoting, line continuations) and a parser for the flag subset browsers
  emit (`-H -X -d --data-* --data-urlencode -b -u -A -e -G -I --json
  --compressed`, ignorable transfer options warned about, unsupported
  flags rejected).
- Raw HTTP/1.1 message input (origin- and absolute-form targets, explicit
  `Host` override) from files or stdin, plus bare-URL and pre-tokenized
  `reqmin curl …` invocation.
- Oracles: `--expect-status`, repeatable `--expect-body-contains`,
  `--expect-body-regex`, `--expect-header`; with no flags the oracle binds
  to the baseline status code.
- Faithful replay transport: no injected `Accept-Encoding` or default
  `User-Agent`, redirects reported instead of followed, per-request
  timeout, `--max-requests` budget, and a response cache so repeat
  configurations cost nothing.
- Output as a copy-pasteable curl one-liner, a raw HTTP message, or a
  JSON report (`--format`, `--out`), with kept/removed item summaries on
  stderr, `--keep` pinning globs, `--only` dimension filters, and
  `--dry-run` enumeration.
- Runnable examples (`examples/copied.curl`, `examples/demo-server`,
  `examples/reduce.sh`) and a design doc (`docs/reduction.md`).
- 92 deterministic offline tests (pure ddmin fixtures, loopback httptest
  servers, in-process CLI runs) and `scripts/smoke.sh`.

[0.1.0]: https://github.com/JaydenCJ/reqmin/releases/tag/v0.1.0
