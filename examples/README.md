# reqmin examples

Everything here runs offline against a loopback server.

| File | What it shows |
|---|---|
| `copied.curl` | A realistic browser "Copy as cURL" capture: 14 headers, 4 cookies, 4 query params — of which exactly two matter. |
| `demo-server/` | The tiny API behind the quickstart and the smoke test; it checks only `Authorization` and `?user=`. |
| `reduce.sh` | End-to-end demo: builds both binaries, starts the server on a free port, dry-runs, then minimizes `copied.curl`. |

Run the whole demo:

```bash
bash examples/reduce.sh
```

Or step by step:

```bash
go build -o reqmin ./cmd/reqmin
go run ./examples/demo-server &          # listens on 127.0.0.1:8641
./reqmin --dry-run examples/copied.curl  # list the 22 removable items
./reqmin examples/copied.curl            # ~30 loopback probes later: 2 kept
```

The same input also works as a raw HTTP message, from a file or stdin —
see `reqmin --help` for the input formats and oracle flags.
