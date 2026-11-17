package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/thelab3507/bs-checker/internal/analysis"
	"github.com/thelab3507/bs-checker/internal/results"
)

func analyze(args []string, w io.Writer) error {
	var in, out, corrDir string
	var seed uint64
	var resamples int
	var danger float64
	var rules, quants list
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	fs.SetOutput(w)
	fs.StringVar(&in, "results", "", "results JSONL from bsmeter run (required)")
	fs.StringVar(&out, "out", "analysis.json", "analysis JSON to write")
	fs.StringVar(&corrDir, "corrections-dir", "", "also write one correction file per run into this directory")
	fs.Uint64Var(&seed, "seed", 1, "seed for the half split and the bootstrap")
	fs.IntVar(&resamples, "resamples", 1000, "bootstrap resamples")
	fs.Float64Var(&danger, "danger", 0.95, "danger-zone confidence threshold")
	fs.Var(&rules, "cluster", "set=regexp: resample whole clusters, key = first capture group of the question id (repeatable; default "+analysis.DefaultClusterRule+")")
	fs.Var(&quants, "quant", "analyse only rows of this quantisation, such as Q4_K_M (repeatable; default every quantisation in the file)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if in == "" {
		return fmt.Errorf("-results is required")
	}
	if len(rules) == 0 {
		rules = list{analysis.DefaultClusterRule}
	}
	opts := analysis.Options{Seed: seed, Resamples: resamples, Danger: danger}
	for _, r := range rules {
		rule, err := analysis.ParseClusterRule(r)
		if err != nil {
			return err
		}
		opts.Rules = append(opts.Rules, rule)
	}

	rows, err := results.LoadAll(in)
	if err != nil {
		return err
	}
	if rows, err = onlyQuants(rows, quants, in); err != nil {
		return err
	}
	doc, err := analysis.Build(rows, opts)
	if err != nil {
		return err
	}
	if err := ensureDir(out); err != nil {
		return err
	}
	if err := analysis.WriteFile(out, doc); err != nil {
		return err
	}
	var corrections int
	if corrDir != "" {
		if err := os.MkdirAll(corrDir, 0o755); err != nil {
			return err
		}
		for _, r := range doc.Runs {
			if r.Correction == nil {
				continue
			}
			f := analysis.CorrectionFile{Method: r.Correction.Method, Params: r.Correction.Params}
			if err := analysis.WriteFile(filepath.Join(corrDir, analysis.CorrectionName(r)), f); err != nil {
				return err
			}
			corrections++
		}
	}
	summarise(w, doc, out, corrections)
	return nil
}

// onlyQuants keeps the rows of the named quantisations. Naming one the file
// has no rows for is an error, so a typo cannot give an empty report.
func onlyQuants(rows []results.Result, quants []string, path string) ([]results.Result, error) {
	if len(quants) == 0 {
		return rows, nil
	}
	want := map[string]bool{}
	for _, q := range quants {
		want[q] = false
	}
	var kept []results.Result
	for _, r := range rows {
		if _, ok := want[r.Quant]; ok {
			want[r.Quant] = true
			kept = append(kept, r)
		}
	}
	for _, q := range quants {
		if !want[q] {
			return nil, fmt.Errorf("-quant %s: %s has no rows of that quantisation", q, path)
		}
	}
	return kept, nil
}

func summarise(w io.Writer, doc analysis.Doc, out string, corrections int) {
	var runWarnings int
	for _, r := range doc.Runs {
		runWarnings += len(r.Warnings)
	}
	fmt.Fprintf(w, "%d runs, %d corrections -> %s\n", len(doc.Runs), corrections, out)
	if n := len(doc.Warnings) + runWarnings; n > 0 {
		fmt.Fprintf(w, "%d warnings, see \"warnings\" in %s\n", n, out)
	}
}
