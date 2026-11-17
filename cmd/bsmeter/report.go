package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/thelab3507/bs-checker/internal/analysis"
	"github.com/thelab3507/bs-checker/internal/report"
)

func reportCmd(args []string, w io.Writer) error {
	var in, out, csvDir string
	var opts report.Options
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(w)
	fs.StringVar(&in, "analysis", "analysis.json", "analysis JSON from bsmeter analyze")
	fs.StringVar(&out, "out", "report.html", "HTML report to write")
	fs.StringVar(&csvDir, "csv-dir", "", "also write runs.csv into this directory")
	fs.StringVar(&opts.Title, "title", "", "page title (default \"BS Meter report\")")
	fs.StringVar(&opts.Note, "note", "", "a line shown under the title, such as \"Synthetic data\"")
	if err := fs.Parse(args); err != nil {
		return err
	}
	doc, err := analysis.Read(in)
	if err != nil {
		return err
	}
	page, err := report.Render(doc, opts)
	if err != nil {
		return err
	}
	if err := ensureDir(out); err != nil {
		return err
	}
	if err := os.WriteFile(out, page, 0o644); err != nil {
		return err
	}
	if csvDir != "" {
		if err := os.MkdirAll(csvDir, 0o755); err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := report.WriteRunsCSV(&buf, doc); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(csvDir, "runs.csv"), buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(w, "%d runs -> %s (%d KB)\n", len(doc.Runs), out, (len(page)+1023)/1024)
	return nil
}
