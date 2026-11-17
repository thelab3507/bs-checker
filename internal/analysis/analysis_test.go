package analysis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"strings"
	"testing"

	"github.com/thelab3507/bs-checker/internal/results"
)

type item struct {
	qid   string
	q     float64
	right bool
}

// world draws n questions, each answered correctly with probability q (uniform
// in (0.5, 0.999)). Two runs built from one world see the same questions and
// the same outcomes, as a 4-bit and an 8-bit run of one model would.
func world(n int, prefix string, seed uint64) []item {
	rng := rand.New(rand.NewPCG(seed, 1))
	w := make([]item, n)
	for i := range w {
		q := 0.5 + 0.499*rng.Float64()
		w[i] = item{fmt.Sprintf("%s-%04d", prefix, i), q, rng.Float64() < q}
	}
	return w
}

func logit(p float64) float64 { return math.Log(p / (1 - p)) }

// rowsFor reports confidence sigmoid(sharp * logit(q)): sharp 1 is calibrated,
// above 1 overconfident.
func rowsFor(model, quant, host, set string, w []item, sharp float64) []results.Result {
	rows := make([]results.Result, len(w))
	for i, it := range w {
		conf := 1 / (1 + math.Exp(-sharp*logit(it.q)))
		rows[i] = results.Result{
			Model: model, Quant: quant, Host: host, Set: set, QID: it.qid, Method: "tokenprob",
			Chosen: "Yes", Probs: map[string]float64{"Yes": conf, "No": 1 - conf},
			PPos: conf, PChosen: conf, AnswerMass: 1, Correct: it.right,
		}
	}
	return rows
}

func opts() Options {
	return Options{Seed: 1, Resamples: 200, Danger: 0.95,
		Rules: []ClusterRule{mustRule(DefaultClusterRule)}}
}

func mustRule(s string) ClusterRule {
	r, err := ParseClusterRule(s)
	if err != nil {
		panic(err)
	}
	return r
}

func build(t *testing.T, rows []results.Result, o Options) Doc {
	t.Helper()
	d, err := Build(rows, o)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func find(t *testing.T, d Doc, model, quant, host, set string) Run {
	t.Helper()
	for _, r := range d.Runs {
		if r.Model == model && r.Quant == quant && r.Host == host && r.Set == set {
			return r
		}
	}
	t.Fatalf("no run %s %s %s %s", model, quant, host, set)
	return Run{}
}

func hasWarning(r Run, sub string) bool {
	for _, w := range r.Warnings {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

func TestRunsGroupByModelQuantHostMethodSet(t *testing.T) {
	w := world(200, "q", 1)
	var rows []results.Result
	rows = append(rows, rowsFor("m", "Q4_K_M", "mac", "s", w, 1)...)
	rows = append(rows, rowsFor("m", "Q8_0", "mac", "s", w, 1)...)
	rows = append(rows, rowsFor("m", "Q4_K_M", "win", "s", w, 1)...)
	d := build(t, rows, opts())
	if len(d.Runs) != 3 {
		t.Fatalf("runs = %d, want 3", len(d.Runs))
	}
	if len(d.Warnings) != 0 {
		t.Fatalf("runs on different hosts or quantisations need no pairing, but the document warns: %v", d.Warnings)
	}
}

func TestDocCarriesNoComparisons(t *testing.T) {
	w := world(200, "q", 1)
	rows := append(rowsFor("m", "Q4_K_M", "h", "s", w, 1), rowsFor("m", "Q8_0", "h", "s", w, 1)...)
	data, err := Marshal(build(t, rows, opts()))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("comparisons")) || bytes.Contains(data, []byte("n_paired")) {
		t.Fatal("the analysis still holds a paired comparison")
	}
}

func TestOutputIsByteIdenticalForTheSameSeedAndInput(t *testing.T) {
	w := world(300, "q", 2)
	rows := append(rowsFor("m", "Q4_K_M", "h", "s", w, 2), rowsFor("m", "Q8_0", "h", "s", w, 1)...)
	a, _ := Marshal(build(t, rows, opts()))
	// the same rows in a different order must not change a byte
	rev := slicesReversed(rows)
	b, _ := Marshal(build(t, rev, opts()))
	if !bytes.Equal(a, b) {
		t.Fatal("output depends on row order or is not deterministic")
	}
	o2 := opts()
	o2.Seed = 2
	c, _ := Marshal(build(t, rows, o2))
	if bytes.Equal(a, c) {
		t.Fatal("a different seed should change the intervals")
	}
}

func slicesReversed(rows []results.Result) []results.Result {
	out := make([]results.Result, len(rows))
	for i, r := range rows {
		out[len(rows)-1-i] = r
	}
	return out
}

func TestAddingARunLeavesTheOthersUnchanged(t *testing.T) {
	w := world(300, "q", 3)
	base := append(rowsFor("m", "Q4_K_M", "h", "s", w, 2), rowsFor("m", "Q8_0", "h", "s", w, 1)...)
	before := build(t, base, opts())
	after := build(t, append(base, rowsFor("other", "Q4_K_M", "h", "s", world(300, "z", 9), 1)...), opts())

	for _, quant := range []string{"Q4_K_M", "Q8_0"} {
		x, _ := Marshal(find(t, before, "m", quant, "h", "s"))
		y, _ := Marshal(find(t, after, "m", quant, "h", "s"))
		if !bytes.Equal(x, y) {
			t.Fatalf("%s run changed when an unrelated run was added", quant)
		}
	}
}

func TestOverconfidentRunGetsACorrectionThatHelpsHeldOutData(t *testing.T) {
	w := world(3000, "q", 4)
	d := build(t, rowsFor("m", "Q4_K_M", "h", "s", w, 2), opts())
	r := d.Runs[0]
	if r.Correction == nil {
		t.Fatalf("no correction; warnings: %v", r.Warnings)
	}
	c := r.Correction
	if T := float64(c.Params["T"]); math.Abs(T-2) > 0.3 {
		t.Fatalf("T = %v, want near 2", T)
	}
	if c.ECEAfter >= c.ECEBefore || c.NLLAfter >= c.NLLBefore {
		t.Fatalf("held-out ECE %v -> %v, NLL %v -> %v, want both to fall", c.ECEBefore, c.ECEAfter, c.NLLBefore, c.NLLAfter)
	}
	if c.FitN+c.EvalN != r.N {
		t.Fatalf("fit %d + eval %d != n %d", c.FitN, c.EvalN, r.N)
	}
}

func TestClusterRuleGroupsStoriesAndLeavesOtherSetsAlone(t *testing.T) {
	var news []results.Result
	for _, story := range []string{"fed", "nba", "opec"} {
		for _, it := range world(8, "news-"+story, 1) {
			news = append(news, rowsFor("m", "Q4_K_M", "h", "fresh-news", []item{it}, 1)...)
		}
	}
	plain := rowsFor("m", "Q4_K_M", "h", "boolq", world(40, "boolq", 1), 1)
	d := build(t, append(news, plain...), opts())

	if got := find(t, d, "m", "Q4_K_M", "h", "fresh-news"); got.N != 24 || got.NClusters != 3 {
		t.Fatalf("fresh-news n=%d clusters=%d, want 24 and 3", got.N, got.NClusters)
	}
	if got := find(t, d, "m", "Q4_K_M", "h", "boolq"); got.NClusters != 40 {
		t.Fatalf("boolq clusters = %d, want one per question", got.NClusters)
	}
	if d.ClusterBy["boolq"] != "qid" || !strings.Contains(d.ClusterBy["fresh-news"], "news-") {
		t.Fatalf("cluster_by = %v", d.ClusterBy)
	}
}

func TestParseClusterRuleErrors(t *testing.T) {
	for _, bad := range []string{"", "nogroup", "s=", "=x", "s=(unclosed", "s=no-capture-group"} {
		if _, err := ParseClusterRule(bad); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}

func TestWarnings(t *testing.T) {
	t.Run("small run: few answers, few clusters, nothing to fit", func(t *testing.T) {
		r := build(t, rowsFor("m", "Q4_K_M", "h", "s", world(20, "q", 1), 1), opts()).Runs[0]
		for _, want := range []string{"only 20 answers", "only 20 clusters", "no correction fitted"} {
			if !hasWarning(r, want) {
				t.Errorf("missing warning %q in %v", want, r.Warnings)
			}
		}
		if r.Correction != nil {
			t.Fatal("a 10-answer fit half must not produce a correction")
		}
	})
	t.Run("low answer mass is kept, counted and warned", func(t *testing.T) {
		rows := rowsFor("m", "Q4_K_M", "h", "s", world(200, "q", 1), 1)
		for i := 0; i < 5; i++ {
			rows[i].AnswerMass = 0.2
		}
		r := build(t, rows, opts()).Runs[0]
		if r.N != 200 || r.LowMassN != 5 || !hasWarning(r, "5 answers put under 90%") {
			t.Fatalf("n=%d low=%d warnings=%v", r.N, r.LowMassN, r.Warnings)
		}
	})
	t.Run("a danger zone with no errors says the interval is not the whole story", func(t *testing.T) {
		rows := rowsFor("m", "Q4_K_M", "h", "s", world(200, "q", 1), 1)
		for i := range rows {
			rows[i].Correct, rows[i].PChosen = true, 0.99
		}
		r := build(t, rows, opts()).Runs[0]
		if r.DangerZone.N != 200 || r.DangerZone.Wrong != 0 || !hasWarning(r, "rule of three") || !hasWarning(r, "about 1.5%") {
			t.Fatalf("danger %+v warnings %v", r.DangerZone, r.Warnings)
		}
	})
	t.Run("danger zone counts its wrong answers", func(t *testing.T) {
		rows := rowsFor("m", "Q4_K_M", "h", "s", world(200, "q", 1), 1)
		for i := range rows {
			rows[i].Correct, rows[i].PChosen = i >= 4, 0.99
		}
		if d := build(t, rows, opts()).Runs[0].DangerZone; d.N != 200 || d.Wrong != 4 {
			t.Fatalf("danger %+v", d)
		}
	})
	t.Run("all-correct data pins T at its bound, so no correction", func(t *testing.T) {
		rows := rowsFor("m", "Q4_K_M", "h", "s", world(200, "q", 1), 1)
		for i := range rows {
			rows[i].Correct, rows[i].PChosen = true, 0.9
		}
		r := build(t, rows, opts()).Runs[0]
		if r.Correction != nil || !hasWarning(r, "search bound") {
			t.Fatalf("correction=%v warnings=%v", r.Correction, r.Warnings)
		}
	})
}

func TestBadOptions(t *testing.T) {
	if _, err := Build(nil, opts()); err == nil {
		t.Fatal("no rows: want error")
	}
	o := opts()
	o.Resamples = 10
	if _, err := Build(rowsFor("m", "Q4_K_M", "h", "s", world(10, "q", 1), 1), o); err == nil {
		t.Fatal("10 resamples: want error")
	}
}

func TestEmptyBandsAndDangerZoneAreNullInJSON(t *testing.T) {
	rows := rowsFor("m", "Q4_K_M", "h", "s", world(200, "q", 1), 1)
	for i := range rows {
		rows[i].PChosen = 0.6
	}
	data, err := Marshal(build(t, rows, opts()))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Runs []struct {
			Bands []struct {
				MeanConf *float64 `json:"mean_conf"`
			} `json:"bands"`
			DangerZone struct {
				N       int      `json:"n"`
				HitRate *float64 `json:"hit_rate"`
			} `json:"danger_zone"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, data)
	}
	r := doc.Runs[0]
	if r.Bands[9].MeanConf != nil || r.DangerZone.N != 0 || r.DangerZone.HitRate != nil {
		t.Fatalf("empty band and empty danger zone must be null: %+v", r)
	}
	if strings.Contains(string(data), "NaN") {
		t.Fatal("NaN leaked into the output")
	}
}

// testdata/prototype.json was computed by an independent Python script from
// examples/results.example.jsonl. The
// point estimates must agree; intervals and T differ because the random
// generator and the optimiser differ.
func TestMatchesThePrototypeOnTheExample(t *testing.T) {
	rows, err := results.LoadAll("../../examples/results.example.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	o := opts()
	o.Seed, o.Resamples = 1234, 1000
	got := build(t, rows, o)

	raw, err := os.ReadFile("testdata/prototype.json")
	if err != nil {
		t.Fatal(err)
	}
	var want struct {
		Runs []struct {
			Model, Quant, Host, Set string
			N                       int
			Accuracy, ECE           struct{ Value float64 }
			Bands                   []struct {
				Lo, Hi   float64
				N        int
				MeanConf *float64 `json:"mean_conf"`
				HitRate  *float64 `json:"hit_rate"`
			}
			DangerZone struct {
				N       int
				HitRate *float64 `json:"hit_rate"`
			} `json:"danger_zone"`
		}
	}
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if len(want.Runs) == 0 {
		t.Fatal("example has no runs")
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-5 }
	for _, w := range want.Runs {
		g := find(t, got, w.Model, w.Quant, w.Host, w.Set)
		if g.N != w.N || !near(float64(g.Accuracy.Value), w.Accuracy.Value) || !near(float64(g.ECE.Value), w.ECE.Value) {
			t.Errorf("%s %s: n/accuracy/ece = %d %v %v, want %d %v %v", w.Model, w.Quant, g.N, g.Accuracy.Value, g.ECE.Value, w.N, w.Accuracy.Value, w.ECE.Value)
		}
		if g.DangerZone.N != w.DangerZone.N {
			t.Errorf("%s %s: danger n = %d, want %d", w.Model, w.Quant, g.DangerZone.N, w.DangerZone.N)
		}
		if w.DangerZone.HitRate != nil && !near(float64(g.DangerZone.HitRate), *w.DangerZone.HitRate) {
			t.Errorf("%s %s: danger hit rate = %v, want %v", w.Model, w.Quant, g.DangerZone.HitRate, *w.DangerZone.HitRate)
		}
		if len(g.Bands) != len(w.Bands) {
			t.Fatalf("bands = %d, want %d", len(g.Bands), len(w.Bands))
		}
		for i, wb := range w.Bands {
			gb := g.Bands[i]
			if gb.N != wb.N || !near(float64(gb.Lo), wb.Lo) || !near(float64(gb.Hi), wb.Hi) {
				t.Errorf("%s %s band %d = %+v, want %+v", w.Model, w.Quant, i, gb, wb)
			}
			if wb.MeanConf != nil && (!near(float64(gb.MeanConf), *wb.MeanConf) || !near(float64(gb.HitRate), *wb.HitRate)) {
				t.Errorf("%s %s band %d mean/hit = %v %v, want %v %v", w.Model, w.Quant, i, gb.MeanConf, gb.HitRate, *wb.MeanConf, *wb.HitRate)
			}
		}
	}
}

func TestCorrectionNameIsFileSafeAndUniquePerRun(t *testing.T) {
	a := CorrectionName(Run{Model: "gemma-4-e2b-it", Quant: "Q4_K_M", Host: "mac-metal", Set: "boolq"})
	b := CorrectionName(Run{Model: "gemma-4-e2b-it", Quant: "Q4_K_M", Host: "win-cuda", Set: "boolq"})
	if a == b || a != "gemma-4-e2b-it-Q4_K_M-mac-metal-boolq.json" {
		t.Fatalf("names %q and %q", a, b)
	}
	if n := CorrectionName(Run{Model: "a/b c", Quant: "Q:4", Host: "h", Set: "s"}); strings.ContainsAny(n, "/ :") {
		t.Fatalf("%q is not file safe", n)
	}
}

func TestGapSaysWhichWayAModelIsWrong(t *testing.T) {
	w := world(800, "q", 5)
	rows := append(rowsFor("m", "Q4_K_M", "h", "s", w, 2), rowsFor("m", "Q8_0", "h", "s", w, 0.5)...)
	rows = append(rows, rowsFor("m", "Q6_K", "h", "s", w, 1)...)
	d := build(t, rows, opts())
	if g := find(t, d, "m", "Q4_K_M", "h", "s").Gap; g.CI95[0] <= 0 {
		t.Fatalf("overconfident gap = %+v, want an interval above 0", g)
	}
	if g := find(t, d, "m", "Q8_0", "h", "s").Gap; g.CI95[1] >= 0 {
		t.Fatalf("underconfident gap = %+v, want an interval below 0", g)
	}
	if g := find(t, d, "m", "Q6_K", "h", "s").Gap; g.CI95[0] > 0 || g.CI95[1] < 0 {
		t.Fatalf("calibrated gap = %+v, want an interval spanning 0", g)
	}
}

func TestReadRoundTripsADocAndKeepsNullsNull(t *testing.T) {
	rows := rowsFor("m", "Q4_K_M", "h", "s", world(200, "q", 1), 1)
	for i := range rows {
		rows[i].PChosen = 0.6
	}
	want := build(t, rows, opts())
	path := t.TempDir() + "/a.json"
	if err := WriteFile(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !math.IsNaN(float64(got.Runs[0].Bands[9].MeanConf)) || !math.IsNaN(float64(got.Runs[0].DangerZone.HitRate)) {
		t.Fatal("null must read back as NaN, not 0")
	}
	a, _ := Marshal(want)
	b, _ := Marshal(got)
	if !bytes.Equal(a, b) {
		t.Fatal("a doc did not survive write then read")
	}
}

func TestReadRefusesAStaleOrForeignFile(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]struct{ body, want string }{
		"version": {`{"format_version": 2, "runs": []}`, "format_version 2"},
		"no gap":  {`{"format_version": 1, "runs": [{"model": "m"}]}`, "no \"gap\""},
		"garbage": {`not json`, "invalid character"},
	}
	for name, c := range cases {
		path := dir + "/" + name + ".json"
		os.WriteFile(path, []byte(c.body), 0o644)
		if _, err := Read(path); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, c.want)
		}
	}
	if _, err := Read(dir + "/missing.json"); err == nil {
		t.Error("a missing file must error")
	}
}
