// Package report renders the outcome of a minimization: a human-readable
// summary for stderr and a machine-readable JSON document for --format json.
package report

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/JaydenCJ/reqmin/internal/items"
	"github.com/JaydenCJ/reqmin/internal/version"
)

// Data collects everything a report needs.
type Data struct {
	BaselineStatus int
	OracleDesc     string
	Plan           *items.Plan
	Keep           []bool // final keep mask, len == len(Plan.Items)
	RequestsSent   int
	CacheHits      int
	BudgetHit      bool
	FinalCurl      string
	FinalRaw       string
}

func (d *Data) kept(i int) bool { return d.Plan.Items[i].Forced || d.Keep[i] }

func (d *Data) keptCount() int {
	n := 0
	for i := range d.Plan.Items {
		if d.kept(i) {
			n++
		}
	}
	return n
}

// ItemsSummary renders "17 removable (13 headers, 1 query param, 2 cookies)".
func ItemsSummary(p *items.Plan) string {
	counts := p.Counts()
	labels := map[items.Kind]string{
		items.KindHeader: "header",
		items.KindQuery:  "query param",
		items.KindCookie: "cookie",
		items.KindForm:   "form field",
		items.KindJSON:   "json key",
		items.KindBody:   "raw body", // at most one; never pluralized
	}
	out := fmt.Sprintf("%d removable (", len(p.Items))
	first := true
	for _, k := range items.AllKinds {
		if counts[k] == 0 {
			continue
		}
		if !first {
			out += ", "
		}
		label := labels[k]
		if counts[k] != 1 && k != items.KindBody {
			label += "s"
		}
		out += fmt.Sprintf("%d %s", counts[k], label)
		first = false
	}
	return out + ")"
}

// Text writes the human summary. With verbose, removed items are listed too.
func Text(w io.Writer, d *Data, verbose bool) {
	fmt.Fprintf(w, "baseline: status %d satisfies oracle (%s)\n", d.BaselineStatus, d.OracleDesc)
	fmt.Fprintf(w, "items: %s\n", ItemsSummary(d.Plan))
	fmt.Fprintf(w, "probes: %d requests sent, %d answered from cache\n", d.RequestsSent, d.CacheHits)
	if d.BudgetHit {
		fmt.Fprintf(w, "warning: request budget exhausted — result is reduced but may not be 1-minimal\n")
	}
	noun := "items"
	if len(d.Plan.Items) == 1 {
		noun = "item"
	}
	fmt.Fprintf(w, "result: kept %d of %d %s\n", d.keptCount(), len(d.Plan.Items), noun)
	for i, it := range d.Plan.Items {
		if d.kept(i) {
			note := ""
			if it.Forced {
				note = "   (pinned by --keep)"
			}
			fmt.Fprintf(w, "  kept     %-7s %s%s\n", it.Kind, it.Name, note)
		}
	}
	if verbose {
		for i, it := range d.Plan.Items {
			if !d.kept(i) {
				fmt.Fprintf(w, "  removed  %-7s %s\n", it.Kind, it.Name)
			}
		}
	}
}

type jsonItem struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Pinned bool   `json:"pinned,omitempty"`
}

type jsonReport struct {
	Version        string     `json:"version"`
	BaselineStatus int        `json:"baseline_status"`
	Oracle         string     `json:"oracle"`
	ItemsTotal     int        `json:"items_total"`
	Kept           []jsonItem `json:"kept"`
	Removed        []jsonItem `json:"removed"`
	RequestsSent   int        `json:"requests_sent"`
	CacheHits      int        `json:"cache_hits"`
	BudgetHit      bool       `json:"budget_exhausted,omitempty"`
	MinimalCurl    string     `json:"minimal_curl"`
	MinimalRaw     string     `json:"minimal_raw"`
}

// JSON renders the full machine-readable report.
func JSON(d *Data) ([]byte, error) {
	rep := jsonReport{
		Version:        version.Version,
		BaselineStatus: d.BaselineStatus,
		Oracle:         d.OracleDesc,
		ItemsTotal:     len(d.Plan.Items),
		Kept:           []jsonItem{},
		Removed:        []jsonItem{},
		RequestsSent:   d.RequestsSent,
		CacheHits:      d.CacheHits,
		BudgetHit:      d.BudgetHit,
		MinimalCurl:    d.FinalCurl,
		MinimalRaw:     d.FinalRaw,
	}
	for i, it := range d.Plan.Items {
		ji := jsonItem{Kind: string(it.Kind), Name: it.Name, Pinned: it.Forced}
		if d.kept(i) {
			rep.Kept = append(rep.Kept, ji)
		} else {
			rep.Removed = append(rep.Removed, ji)
		}
	}
	return json.MarshalIndent(rep, "", "  ")
}
