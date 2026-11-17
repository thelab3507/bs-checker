package main

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/thelab3507/bs-checker/internal/qset"
	"github.com/thelab3507/bs-checker/internal/results"
)

// These tests guard the numbers published in results/. They fail if the raw
// answers, the analysis built from them, or the report built from that analysis
// stop agreeing, so a reader who reruns the commands in results/README.md gets
// what the writeup says.

const (
	publishedDir      = "../../results"
	publishedResults  = publishedDir + "/results-mac-metal.jsonl"
	publishedAnalysis = publishedDir + "/analysis-mac-metal.json"
	publishedReport   = publishedDir + "/report-mac-metal.html"
	publishedTitle    = "BS Meter on mac-metal"
	// publishedQuant is the quantisation the published analysis covers. The raw
	// results file also holds Q8_0 rows from an earlier comparison; they are kept
	// but not analysed.
	publishedQuant = "Q4_K_M"
)

func publishedRows(t *testing.T) []results.Result {
	t.Helper()
	rows, err := results.LoadAll(publishedResults)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

type runKey struct{ model, quant, set string }

func groupRuns(rows []results.Result) map[runKey][]results.Result {
	g := map[runKey][]results.Result{}
	for _, r := range rows {
		k := runKey{r.Model, r.Quant, r.Set}
		g[k] = append(g[k], r)
	}
	return g
}

func TestPublishedResultsAnswerEveryQuestionOnce(t *testing.T) {
	sets := map[string]map[string]bool{}
	for _, name := range []string{"boolq", "sms-spam", "fresh-news"} {
		qs, err := qset.Load(filepath.Join("..", "..", "data", name+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		ids := map[string]bool{}
		for _, q := range qs {
			ids[q.ID] = true
		}
		sets[name] = ids
	}
	groups := groupRuns(publishedRows(t))
	if len(groups) != 15 {
		t.Fatalf("runs = %d, want 15 (5 model and quantisation pairs x 3 sets, including the kept Q8_0 rows)", len(groups))
	}
	for k, rs := range groups {
		want := sets[k.set]
		if len(rs) != len(want) {
			t.Errorf("%v: %d answers, want %d", k, len(rs), len(want))
		}
		for _, r := range rs {
			if !want[r.QID] {
				t.Errorf("%v: answer for unknown question %s", k, r.QID)
			}
			if r.Host != "mac-metal" {
				t.Errorf("%v: host %q", k, r.Host)
			}
		}
	}
}

func TestPublishedResultsAreBelievable(t *testing.T) {
	for k, rs := range groupRuns(publishedRows(t)) {
		right, stuck := 0, 0
		for _, r := range rs {
			if r.Correct {
				right++
			}
			if r.PChosen < 0.51 {
				stuck++
			}
		}
		acc := float64(right) / float64(len(rs))
		if acc < 0.6 || acc > 1 {
			t.Errorf("%v: accuracy %.3f is not believable for a yes/no task", k, acc)
		}
		if share := float64(stuck) / float64(len(rs)); share > 0.05 {
			t.Errorf("%v: %.0f%% of answers sit at 50%% confidence, so the model may not be giving probabilities", k, share*100)
		}
	}
}

func TestPublishedResultsKeepTheQ8Rows(t *testing.T) {
	q8 := 0
	for k := range groupRuns(publishedRows(t)) {
		if k.quant == "Q8_0" {
			q8++
		}
	}
	if q8 != 6 {
		t.Fatalf("Q8_0 runs = %d, want the 6 kept from the earlier comparison (2 models x 3 sets)", q8)
	}
}

func TestPublishedAnalysisCoversThreeModelsAtQ4(t *testing.T) {
	data, err := os.ReadFile(publishedAnalysis)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Runs []struct{ Model, Quant, Set string } `json:"runs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	sets := map[string]map[string]bool{}
	for _, r := range doc.Runs {
		if r.Quant != publishedQuant {
			t.Errorf("%s on %s is %s, want only %s analysed", r.Model, r.Set, r.Quant, publishedQuant)
		}
		if sets[r.Model] == nil {
			sets[r.Model] = map[string]bool{}
		}
		sets[r.Model][r.Set] = true
	}
	for _, model := range []string{"lfm2-d1-2b", "kev-4b", "gemma-4-e2b-it"} {
		if len(sets[model]) != 3 {
			t.Errorf("%s is analysed on %d sets, want 3", model, len(sets[model]))
		}
	}
	if len(doc.Runs) != 9 || len(sets) != 3 {
		t.Errorf("runs = %d over %d models, want 9 over 3", len(doc.Runs), len(sets))
	}
}

func decode(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

// fitTolerance is the tolerance for the fitted correction. Log loss is flat
// at its minimum, so the temperature search can only pin T to about the square
// root of machine precision; chips that round differently land on values that
// differ around the seventh decimal. Every other number must match far closer.
const fitTolerance = 1e-6

// sameNumbers walks two decoded JSON values and reports every difference.
// Numbers match within tol: Apple, Intel and AMD chips round floating point
// differently in the last bit, and the same analysis must still count as
// reproduced.
func sameNumbers(t *testing.T, path string, got, want any, tol float64) {
	t.Helper()
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok || len(g) != len(w) {
			t.Errorf("%s: shape differs", path)
			return
		}
		for k, wv := range w {
			sub := tol
			if k == "correction" {
				sub = fitTolerance
			}
			sameNumbers(t, path+"."+k, g[k], wv, sub)
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			t.Errorf("%s: length differs", path)
			return
		}
		for i := range w {
			sameNumbers(t, path+"[]", g[i], w[i], tol)
		}
	case float64:
		g, ok := got.(float64)
		if !ok || math.Abs(g-w) > tol {
			t.Errorf("%s: %v, want %v", path, got, want)
		}
	default:
		if got != want {
			t.Errorf("%s: %v, want %v", path, got, want)
		}
	}
}

func TestPublishedAnalysisIsWhatTheResultsGive(t *testing.T) {
	out := filepath.Join(t.TempDir(), "a.json")
	if err := analyze([]string{"-results", publishedResults, "-quant", publishedQuant, "-out", out}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	sameNumbers(t, "analysis", decode(t, out), decode(t, publishedAnalysis), 1e-9)
}

func TestPublishedReportIsWhatTheAnalysisGives(t *testing.T) {
	out := filepath.Join(t.TempDir(), "r.html")
	if err := reportCmd([]string{"-analysis", publishedAnalysis, "-out", out, "-title", publishedTitle}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(publishedReport)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("results/report-mac-metal.html is not what bsmeter report makes from results/analysis-mac-metal.json")
	}
}
