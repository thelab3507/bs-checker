package report

import (
	"bytes"
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/thelab3507/bs-checker/internal/analysis"
	"github.com/thelab3507/bs-checker/internal/results"
)

var update = flag.Bool("update", false, "rewrite examples/report.example.html from the synthetic data")

func sigmoid(z float64) float64 { return 1 / (1 + math.Exp(-z)) }
func logit(p float64) float64   { return math.Log(p / (1 - p)) }

func rowFor(model, quant, set, qid string, conf float64, right bool) results.Result {
	return results.Result{
		Model: model, Quant: quant, Host: "synthetic-host", Set: set, QID: qid, Method: "tokenprob",
		Chosen: "Yes", Probs: map[string]float64{"Yes": conf, "No": 1 - conf},
		PPos: conf, PChosen: conf, AnswerMass: 1, Correct: right,
	}
}

// syntheticRows builds a made-up world with known behaviour. Nothing here is a
// measurement of any real model.
//
//	synthetic-model: overconfident (it reports log-odds twice as sharp as the
//	  truth).
//	synthetic-calibrated: calibrated, on the same questions as synthetic-model.
//	synthetic-saturated: sits at 99% to 99.999% confidence and is right 96% of
//	  the time, so it is overconfident.
//
// Each model answers "synthetic-questions" (one question per group) and
// "synthetic-news" (stories of eight questions, ids news-<story>-<nn>), and a
// 30-question "synthetic-tiny" set that is too small to judge.
func syntheticRows() []results.Result {
	rng := rand.New(rand.NewPCG(2026, 4))
	var rows []results.Result
	emit := func(set string, ids []string) {
		for _, id := range ids {
			q := 0.5 + 0.499*rng.Float64()
			right := rng.Float64() < q
			rows = append(rows,
				rowFor("synthetic-model", "Q4_K_M", set, id, sigmoid(2*logit(q)), right),
				rowFor("synthetic-calibrated", "Q4_K_M", set, id, q, right))

			sright := rng.Float64() < 0.96
			c := 1 - math.Pow(10, -(2+3*rng.Float64()))
			rows = append(rows, rowFor("synthetic-saturated", "Q4_K_M", set, id, c, sright))
		}
	}
	var plain, news, tiny []string
	for i := 0; i < 400; i++ {
		plain = append(plain, fmt.Sprintf("q-%03d", i))
	}
	for s := 0; s < 36; s++ {
		for i := 0; i < 8; i++ {
			news = append(news, fmt.Sprintf("news-s%02d-%02d", s, i))
		}
	}
	for i := 0; i < 30; i++ {
		tiny = append(tiny, fmt.Sprintf("t-%02d", i))
	}
	emit("synthetic-questions", plain)
	emit("synthetic-news", news)
	emit("synthetic-tiny", tiny)
	return rows
}

func syntheticDoc(t testing.TB) analysis.Doc {
	t.Helper()
	rule, err := analysis.ParseClusterRule(`synthetic-news=^news-(.+)-\d+$`)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := analysis.Build(syntheticRows(), analysis.Options{
		Seed: 1, Resamples: 300, Danger: 0.95, Rules: []analysis.ClusterRule{rule},
	})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

var sampleOptions = Options{
	Title: "Sample report (synthetic data)",
	Note:  "Everything on this page comes from a made-up world with known behaviour. It measures no real model.",
}

func TestSampleReportMatchesTheCommittedFile(t *testing.T) {
	got, err := Render(syntheticDoc(t), sampleOptions)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "..", "examples", "report.example.html")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; create it with: go test ./internal/report -run Sample -update", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("examples/report.example.html is out of date; regenerate it with: go test ./internal/report -run Sample -update")
	}
}
