# How reqmin reduces a request

This note documents the item model and the search, for contributors and
for anyone deciding whether the tool's answer can be trusted.

## The item model

A request is decomposed into independently removable **items**:

| Kind | Atom | Notes |
|---|---|---|
| `header` | one header line | order and duplicates preserved; `Host` and `Content-Length` are never items (the authority is part of the target, the length is recomputed) |
| `cookie` | one `name=value` piece | all `Cookie` headers are split; survivors are rebuilt into a single header at the original position, and the header disappears when no cookie survives |
| `query` | one raw `key[=value]` segment | segments are kept verbatim — dropping a neighbor never re-encodes `%2F` into `/` |
| `form` | one field of an `application/x-www-form-urlencoded` body | same raw-segment rule |
| `json` | one object key path (`user.address.zip`) | parsed into an ordered tree so surviving members keep their order and number literals; arrays are opaque in v0.1.0 |
| `body` | the entire body | used when the body is neither form nor JSON |

Removing a JSON parent subsumes its children; ddmin discovers that cheaply
because removing an already-removed path is a no-op and the resulting
candidate is byte-identical, so the memo cache answers it without a probe.

## The oracle

A candidate "still reproduces" when every predicate holds against its
response: status code, body substrings, an RE2 body pattern, response
header presence/substrings. With no explicit predicates, reqmin binds the
oracle to the baseline response's status code. The baseline (the full
request) must satisfy the oracle or the run stops with exit code 1 —
minimizing toward a property the original request does not have would be
meaningless.

## The search: ddmin

reqmin runs Zeller's ddmin over the item set, minimizing the *kept* set:

1. Probe the empty set first — very often nothing matters, and that
   answer costs one request.
2. Split the current set into *n* chunks; try each chunk alone, then each
   complement; on success, recurse with the reduction.
3. Refine granularity up to single items. The result is **1-minimal**:
   removing any single kept item breaks reproduction.

Two guarantees make this safe against real servers:

- **Memoization** — identical candidates (by method, URL, headers, body)
  are answered from a cache, so revisited configurations are free.
- **Budget** — `--max-requests` (default 500) caps what a run may cost
  the target; on exhaustion the best reduction so far is emitted with a
  warning that it may not be 1-minimal.

## What ddmin cannot promise

1-minimality is not global minimality: with non-monotonic servers (e.g. a
request that succeeds with either of two API keys but fails with both),
a different minimal set may exist. Interactions between items are handled
— pairs and triples of jointly-required items are found routinely — but
the answer is *a* minimal reproduction, not necessarily the smallest one
in existence. In practice, for the "which of these 40 headers matter"
question, the distinction rarely surfaces.

## Fidelity rules

The transport never edits the candidate: no default `User-Agent`, no
injected `Accept-Encoding`, no followed redirects (a `302` is a result,
not a detour). What reqmin prints at the end is exactly what it sent —
the output curl command replays byte-for-byte.
