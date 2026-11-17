package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func analysed(t *testing.T) (dir, analysisPath string) {
	t.Helper()
	dir = t.TempDir()
	in := filepath.Join(dir, "r.jsonl")
	writeBigResults(t, in)
	analysisPath = filepath.Join(dir, "a.json")
	if err := analyze([]string{"-results", in, "-out", analysisPath, "-resamples", "200"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	return dir, analysisPath
}

func TestReportWritesHTMLAndRunsCSV(t *testing.T) {
	dir, a := analysed(t)
	out, csvDir := filepath.Join(dir, "deep", "report.html"), filepath.Join(dir, "csv")
	var buf bytes.Buffer
	if err := reportCmd([]string{"-analysis", a, "-out", out, "-csv-dir", csvDir, "-title", "My run", "-note", "A note"}, &buf); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(out)
	if err != nil || !strings.Contains(string(page), "<title>My run</title>") || !strings.Contains(string(page), "A note") {
		t.Fatalf("page err %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(csvDir, "runs.csv"))
	if err != nil || !strings.HasPrefix(string(raw), "model,quant,host") {
		t.Errorf("runs.csv: %v %.60q", err, raw)
	}
	if files, _ := filepath.Glob(filepath.Join(csvDir, "*.csv")); len(files) != 1 {
		t.Errorf("csv files = %v, want only runs.csv", files)
	}
	if !strings.Contains(buf.String(), "2 runs ->") {
		t.Errorf("summary = %q", buf.String())
	}
}

func TestReportErrorsAreClear(t *testing.T) {
	dir, a := analysed(t)
	cases := map[string]struct {
		args []string
		want string
	}{
		"missing analysis": {[]string{"-analysis", filepath.Join(dir, "nope.json")}, "nope.json"},
		"unwritable out":   {[]string{"-analysis", a, "-out", filepath.Join(dir, "r.jsonl", "x.html")}, "r.jsonl"},
	}
	stale := filepath.Join(dir, "stale.json")
	os.WriteFile(stale, []byte(`{"format_version": 1, "runs": [{"model": "m"}]}`), 0o644)
	cases["stale analysis"] = struct {
		args []string
		want string
	}{[]string{"-analysis", stale}, "rerun bsmeter analyze"}

	for name, c := range cases {
		err := reportCmd(c.args, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, c.want)
		}
	}
}
