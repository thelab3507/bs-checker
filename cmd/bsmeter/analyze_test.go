package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const example = "../../examples/results.example.jsonl"

func TestAnalyzeWritesDocAndOneCorrectionPerRun(t *testing.T) {
	dir := t.TempDir()
	out, corr := filepath.Join(dir, "sub", "a.json"), filepath.Join(dir, "corr")
	var buf bytes.Buffer
	// 20 answers per run is too few for a correction, so build a bigger file.
	in := filepath.Join(dir, "r.jsonl")
	writeBigResults(t, in)
	if err := analyze([]string{"-results", in, "-out", out, "-corrections-dir", corr, "-seed", "3", "-resamples", "200"}, &buf); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Seed int                             `json:"seed"`
		Runs []struct{ Model, Quant string } `json:"runs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil || doc.Seed != 3 || len(doc.Runs) != 2 {
		t.Fatalf("doc = %+v, %v", doc, err)
	}
	files, _ := filepath.Glob(filepath.Join(corr, "*.json"))
	if len(files) != 2 {
		t.Fatalf("correction files = %v, want 2", files)
	}
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		var c struct {
			Method string
			Params map[string]float64
		}
		if err := json.Unmarshal(raw, &c); err != nil || c.Method != "temperature" || c.Params["T"] <= 0 {
			t.Fatalf("%s = %s (%v)", f, raw, err)
		}
	}
	if !strings.Contains(buf.String(), "2 runs, 2 corrections") || strings.Contains(buf.String(), "comparison") {
		t.Fatalf("summary = %q", buf.String())
	}
}

func analysedQuants(t *testing.T, args ...string) []string {
	t.Helper()
	dir := t.TempDir()
	in, out := filepath.Join(dir, "r.jsonl"), filepath.Join(dir, "a.json")
	writeBigResults(t, in)
	if err := analyze(append([]string{"-results", in, "-out", out, "-resamples", "200"}, args...), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Runs []struct{ Quant string } `json:"runs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var quants []string
	for _, r := range doc.Runs {
		quants = append(quants, r.Quant)
	}
	return quants
}

func TestAnalyzeQuantKeepsOnlyThatQuantisation(t *testing.T) {
	if got := analysedQuants(t, "-quant", "Q4_K_M"); len(got) != 1 || got[0] != "Q4_K_M" {
		t.Fatalf("runs = %v, want only Q4_K_M", got)
	}
	if got := analysedQuants(t); len(got) != 2 {
		t.Fatalf("without -quant: runs = %v, want both quantisations", got)
	}
	if got := analysedQuants(t, "-quant", "Q4_K_M", "-quant", "Q8_0"); len(got) != 2 {
		t.Fatalf("with both named: runs = %v", got)
	}
}

func writeBigResults(t *testing.T, path string) {
	t.Helper()
	var b strings.Builder
	for _, quant := range []string{"Q4_K_M", "Q8_0"} {
		for i := 0; i < 300; i++ {
			conf, right := 0.97, i%5 != 0
			if quant == "Q8_0" {
				conf = 0.85
			}
			line, _ := json.Marshal(map[string]any{
				"model": "m", "quant": quant, "host": "h", "set": "s", "qid": fmt.Sprintf("q%04d", i),
				"method": "tokenprob", "chosen": "Yes", "probs": map[string]float64{"Yes": conf, "No": 1 - conf},
				"p_pos": conf, "p_chosen": conf, "answer_mass": 1, "correct": right,
			})
			b.Write(line)
			b.WriteByte('\n')
		}
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyzeIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json")
	for _, out := range []string{a, b} {
		if err := analyze([]string{"-results", example, "-out", out}, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
	}
	x, _ := os.ReadFile(a)
	y, _ := os.ReadFile(b)
	if !bytes.Equal(x, y) {
		t.Fatal("two runs on the same input differ")
	}
}

func TestAnalyzeFlagErrors(t *testing.T) {
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"no results":        {[]string{}, "-results is required"},
		"unknown quant":     {[]string{"-results", example, "-quant", "Q2_K"}, "-quant Q2_K"},
		"one quant missing": {[]string{"-results", example, "-quant", "Q4_K_M", "-quant", "Q2_K"}, "no rows of that quantisation"},
		"bad cluster rule":  {[]string{"-results", example, "-cluster", "nope"}, "set=regexp"},
		"missing file":      {[]string{"-results", "/no/such/file.jsonl"}, "file.jsonl"},
		"too few resamples": {[]string{"-results", example, "-resamples", "5"}, "-resamples"},
	} {
		t.Run(name, func(t *testing.T) {
			err := analyze(append(c.args, "-out", filepath.Join(t.TempDir(), "a.json")), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
		})
	}
}

func TestAnalyzeRejectsABrokenResultsFileNamingTheLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "r.jsonl")
	os.WriteFile(p, []byte(`{"model":"m","p_chosen":0.2,"probs":{"Yes":0.5,"No":0.5}}`+"\n"), 0o644)
	err := analyze([]string{"-results", p, "-out", filepath.Join(t.TempDir(), "a.json")}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("got %v", err)
	}
}
