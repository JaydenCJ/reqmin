// Package cli wires flag parsing, input loading, the oracle, the runner,
// and the ddmin search into the reqmin command. Run is fully injectable
// (streams and HTTP client), so the whole command is testable in-process
// against loopback servers.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/JaydenCJ/reqmin/internal/curl"
	"github.com/JaydenCJ/reqmin/internal/ddmin"
	"github.com/JaydenCJ/reqmin/internal/items"
	"github.com/JaydenCJ/reqmin/internal/oracle"
	"github.com/JaydenCJ/reqmin/internal/rawhttp"
	"github.com/JaydenCJ/reqmin/internal/report"
	"github.com/JaydenCJ/reqmin/internal/request"
	"github.com/JaydenCJ/reqmin/internal/runner"
	"github.com/JaydenCJ/reqmin/internal/version"
)

// Exit codes, part of the CLI contract.
const (
	ExitOK      = 0 // minimized successfully
	ExitNoRepro = 1 // the full request does not satisfy the oracle
	ExitUsage   = 2 // bad flags or input
	ExitRuntime = 3 // network or I/O failure mid-run
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ", ") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

const usageText = `reqmin %s — shrink an HTTP request to the parts that matter

Usage:
  reqmin [flags] <input>
  reqmin [flags] curl <curl args...>

Input (auto-detected):
  a file containing a curl command line or a raw HTTP/1.1 request,
  "-" to read the same from stdin, a quoted "curl ..." string, or a bare URL.

Oracle flags (ANDed; default: same status code as the baseline response):
  --expect-status N            response status must equal N
  --expect-body-contains S     response body must contain S (repeatable)
  --expect-body-regex RE       response body must match RE2 pattern
  --expect-header 'K: v'       response header K must contain v (repeatable)

Search flags:
  --keep GLOB       pin items matching GLOB, e.g. 'authorization' or
                    'header:x-*' (repeatable, case-insensitive)
  --only KINDS      restrict to comma-separated kinds:
                    headers,query,cookies,form,json,body
  --max-requests N  request budget (default 500; 0 = unlimited)
  --timeout D       per-request timeout (default 10s)

Output flags:
  --format F        curl | raw | json   (default curl)
  --out FILE        write the minimized request to FILE instead of stdout
  --dry-run         list removable items without sending anything
  --verbose         also list removed items in the report
  -q, --quiet       suppress the report on stderr
  --scheme S        scheme for origin-form raw requests (default http)
  --version         print version and exit

Exit codes: 0 minimized · 1 baseline does not reproduce · 2 usage · 3 runtime
`

// Run executes the CLI. client may be nil (a default transport is built);
// tests inject an httptest client. The return value is the process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, client *http.Client) int {
	fs := flag.NewFlagSet("reqmin", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintf(stderr, usageText, version.Version) }

	var (
		expectStatus   = fs.Int("expect-status", 0, "")
		expectRegex    = fs.String("expect-body-regex", "", "")
		expectContains multiFlag
		expectHeaders  multiFlag
		keepPatterns   multiFlag
		only           = fs.String("only", "", "")
		format         = fs.String("format", "curl", "")
		outPath        = fs.String("out", "", "")
		scheme         = fs.String("scheme", "http", "")
		timeout        = fs.Duration("timeout", 10*time.Second, "")
		maxRequests    = fs.Int("max-requests", 500, "")
		dryRun         = fs.Bool("dry-run", false, "")
		verbose        = fs.Bool("verbose", false, "")
		quiet          = fs.Bool("quiet", false, "")
		showVersion    = fs.Bool("version", false, "")
	)
	fs.Var(&expectContains, "expect-body-contains", "")
	fs.Var(&expectHeaders, "expect-header", "")
	fs.Var(&keepPatterns, "keep", "")
	fs.BoolVar(quiet, "q", false, "")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK // -h / --help: usage was requested, not misused
		}
		return ExitUsage
	}
	if *showVersion {
		fmt.Fprintf(stdout, "reqmin %s\n", version.Version)
		return ExitOK
	}
	if *format != "curl" && *format != "raw" && *format != "json" {
		fmt.Fprintf(stderr, "reqmin: unknown --format %q (want curl, raw, or json)\n", *format)
		return ExitUsage
	}

	req, warns, err := loadInput(fs.Args(), stdin, *scheme)
	if err != nil {
		fmt.Fprintf(stderr, "reqmin: %v\n", err)
		fmt.Fprintf(stderr, "run 'reqmin' with no arguments for usage\n")
		return ExitUsage
	}
	if !*quiet {
		for _, w := range warns {
			fmt.Fprintf(stderr, "note: %s\n", w)
		}
	}

	onlySet, err := items.ParseOnly(*only)
	if err != nil {
		fmt.Fprintf(stderr, "reqmin: %v\n", err)
		return ExitUsage
	}
	plan, err := items.New(req, items.Options{Only: onlySet, Keep: keepPatterns})
	if err != nil {
		fmt.Fprintf(stderr, "reqmin: %v\n", err)
		return ExitUsage
	}

	if *dryRun {
		printDryRun(stdout, plan)
		return ExitOK
	}

	orc, err := oracle.New(oracle.Config{
		Status:         *expectStatus,
		BodyContains:   expectContains,
		BodyRegex:      *expectRegex,
		HeaderContains: expectHeaders,
	})
	if err != nil {
		fmt.Fprintf(stderr, "reqmin: %v\n", err)
		return ExitUsage
	}

	if client == nil {
		client = runner.DefaultClient(*timeout)
	}
	run := runner.New(client, *maxRequests)

	// Baseline: the full request must satisfy the oracle, or there is
	// nothing to preserve while shrinking.
	allKeep := make([]bool, len(plan.Items))
	for i := range allKeep {
		allKeep[i] = true
	}
	base, err := run.Do(plan.Materialize(allKeep))
	if err != nil {
		fmt.Fprintf(stderr, "reqmin: baseline request failed: %v\n", err)
		return ExitRuntime
	}
	orc.BindBaseline(base.Status)
	if ok, why := orc.Check(base.Status, base.Header, base.Body); !ok {
		fmt.Fprintf(stderr, "reqmin: the full request does not satisfy the oracle: %s\n", why)
		fmt.Fprintf(stderr, "reqmin: nothing to minimize — fix the request or the --expect-* flags first\n")
		return ExitNoRepro
	}

	// Delta-debug the non-pinned items.
	minIdx := plan.Minimizable()
	test := func(sub []bool) (bool, error) {
		full := make([]bool, len(plan.Items))
		for j, idx := range minIdx {
			full[idx] = sub[j]
		}
		res, err := run.Do(plan.Materialize(full))
		if err != nil {
			return false, err
		}
		ok, _ := orc.Check(res.Status, res.Header, res.Body)
		return ok, nil
	}
	subKeep, _, err := ddmin.Minimize(len(minIdx), test)
	budgetHit := false
	if err != nil {
		if errors.Is(err, runner.ErrBudget) {
			budgetHit = true
		} else {
			fmt.Fprintf(stderr, "reqmin: %v\n", err)
			return ExitRuntime
		}
	}
	finalKeep := make([]bool, len(plan.Items))
	for j, idx := range minIdx {
		finalKeep[idx] = subKeep[j]
	}
	final := plan.Materialize(finalKeep)

	data := &report.Data{
		BaselineStatus: base.Status,
		OracleDesc:     orc.Describe(),
		Plan:           plan,
		Keep:           finalKeep,
		RequestsSent:   run.Sent(),
		CacheHits:      run.CacheHits(),
		BudgetHit:      budgetHit,
		FinalCurl:      curl.Render(final),
		FinalRaw:       rawhttp.Render(final),
	}
	if !*quiet {
		report.Text(stderr, data, *verbose)
	}

	var out string
	switch *format {
	case "curl":
		out = data.FinalCurl + "\n"
	case "raw":
		out = data.FinalRaw
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
	case "json":
		j, err := report.JSON(data)
		if err != nil {
			fmt.Fprintf(stderr, "reqmin: %v\n", err)
			return ExitRuntime
		}
		out = string(j) + "\n"
	}
	if *outPath != "" {
		if err := os.WriteFile(*outPath, []byte(out), 0o644); err != nil {
			fmt.Fprintf(stderr, "reqmin: %v\n", err)
			return ExitRuntime
		}
	} else {
		fmt.Fprint(stdout, out)
	}
	return ExitOK
}

func printDryRun(w io.Writer, plan *items.Plan) {
	fmt.Fprintf(w, "KIND     NAME\n")
	for _, it := range plan.Items {
		note := ""
		if it.Forced {
			note = "   (pinned by --keep)"
		}
		fmt.Fprintf(w, "%-8s %s%s\n", it.Kind, it.Name, note)
	}
	fmt.Fprintf(w, "%s\n", report.ItemsSummary(plan))
}

// loadInput resolves the positional arguments into a parsed request.
func loadInput(args []string, stdin io.Reader, scheme string) (*request.Request, []string, error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("no input given")
	}
	// `reqmin curl https://… -H …` — an unquoted, pre-tokenized command.
	if args[0] == "curl" {
		return curl.ParseTokens(args)
	}
	if len(args) > 1 {
		return nil, nil, fmt.Errorf("expected one input argument, got %d (quote the curl command, or lead with the word curl)", len(args))
	}
	src := args[0]

	// A bare URL minimizes a plain GET of itself.
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		return curl.ParseTokens([]string{"curl", src})
	}
	var text string
	switch {
	case src == "-":
		b, err := io.ReadAll(stdin)
		if err != nil {
			return nil, nil, fmt.Errorf("reading stdin: %w", err)
		}
		text = string(b)
	case strings.HasPrefix(strings.TrimSpace(src), "curl"):
		text = src
	default:
		b, err := os.ReadFile(src)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot read input file: %w", err)
		}
		text = string(b)
	}
	if strings.HasPrefix(strings.TrimSpace(text), "curl") {
		return curl.Parse(strings.TrimSpace(text))
	}
	req, err := rawhttp.Parse(text, scheme)
	return req, nil, err
}
