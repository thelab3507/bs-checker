package report

import (
	"bytes"
	"encoding/csv"
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/thelab3507/bs-checker/internal/analysis"
)

func render(t *testing.T, doc analysis.Doc) string {
	t.Helper()
	out, err := Render(doc, Options{Title: "T"})
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestRenderIsByteIdentical(t *testing.T) {
	doc := syntheticDoc(t)
	a, err := Render(doc, sampleOptions)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Render(doc, sampleOptions)
	if !bytes.Equal(a, b) {
		t.Fatal("the same analysis gave different pages")
	}
}

func TestPageIsSelfContained(t *testing.T) {
	page := render(t, syntheticDoc(t))
	for _, banned := range []string{"<script", "<link rel=\"stylesheet\"", "<img", "<iframe", "@import", " src=", "javascript:"} {
		if strings.Contains(page, banned) {
			t.Errorf("page contains %q", banned)
		}
	}
	for _, m := range regexp.MustCompile(`https?://[^"'\s)]*`).FindAllString(page, -1) {
		if m != "http://www.w3.org/2000/svg" {
			t.Errorf("external reference %q", m)
		}
	}
	for _, leak := range []string{"/Users/", "/private/", "/tmp/"} {
		if strings.Contains(page, leak) {
			t.Errorf("page leaks %q", leak)
		}
	}
	if regexp.MustCompile(`20\d\d-\d\d-\d\d`).MatchString(page) {
		t.Error("page carries a date; output must depend on the analysis alone")
	}
	if !strings.Contains(page, `font-family:"Plus Jakarta Sans Variable"`) || !strings.Contains(page, "data:font/woff2;base64,") {
		t.Error("the font must be embedded")
	}
}

func TestEveryRunIsOnThePageWithOneAnswerLine(t *testing.T) {
	doc := syntheticDoc(t)
	page := render(t, doc)
	if got := strings.Count(page, ` data-run="`); got != len(doc.Runs) {
		t.Errorf("run cards = %d, want %d", got, len(doc.Runs))
	}
	if got := strings.Count(page, ` data-answer="`); got != len(doc.Runs) {
		t.Errorf("answer lines = %d, want one per run (%d)", got, len(doc.Runs))
	}
	for _, key := range []string{KeyOver, KeyUnder, KeyHonest, KeyFew} {
		want := 0
		for _, r := range doc.Runs {
			if VerdictFor(r).Key == key {
				want++
			}
		}
		if got := strings.Count(page, ` data-answer="`+key+`"`); got != want {
			t.Errorf("%s answer lines = %d, want %d", key, got, want)
		}
	}
	for _, r := range doc.Runs {
		if !strings.Contains(page, `data-run="`+r.Model+" | "+r.Quant+" | "+r.Host+" | "+r.Set+`"`) {
			t.Errorf("no card for %s %s %s", r.Model, r.Quant, r.Set)
		}
	}
}

func TestSyntheticWorldGivesTheExpectedReadings(t *testing.T) {
	doc := syntheticDoc(t)
	page := render(t, doc)
	for _, want := range []string{
		`class="badge overconfident"`, `class="badge honest"`, `class="badge too-few"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	for _, r := range doc.Runs {
		v := VerdictFor(r).Key
		switch {
		case r.Set == "synthetic-tiny" && v != KeyFew:
			t.Errorf("%s %s: %s", r.Model, r.Set, v)
		case r.Model == "synthetic-model" && r.Set != "synthetic-tiny" && v != KeyOver:
			t.Errorf("the overconfident run on %s was called %s", r.Set, v)
		case r.Model == "synthetic-calibrated" && r.Set == "synthetic-questions" && v != KeyHonest:
			t.Errorf("the calibrated run was called %s", v)
		}
	}
}

func TestColourFollowsTheQuantisation(t *testing.T) {
	doc := syntheticDoc(t)
	for i := range doc.Runs {
		if doc.Runs[i].Model == "synthetic-calibrated" {
			doc.Runs[i].Quant = "Q8_0"
		}
	}
	page := render(t, doc)
	cards := regexp.MustCompile(`(?s)data-run="[^"]*\| (Q4_K_M|Q8_0) \|.*?<span class="key bg-(s\d)">`).FindAllStringSubmatch(page, -1)
	if len(cards) == 0 {
		t.Fatal("found no run cards")
	}
	want := map[string]string{"Q4_K_M": "s1", "Q8_0": "s2"}
	seen := map[string]bool{}
	for _, c := range cards {
		seen[c[1]] = true
		if c[2] != want[c[1]] {
			t.Errorf("%s drawn in %s, want %s everywhere", c[1], c[2], want[c[1]])
		}
	}
	if len(seen) != 2 {
		t.Errorf("saw quantisations %v, want both", seen)
	}
}

func TestPageHasNoQuantisationComparison(t *testing.T) {
	page := render(t, syntheticDoc(t))
	for _, banned := range []string{"data-pair", "data-comparison", "4-bit", "8-bit", "paired"} {
		if strings.Contains(page, banned) {
			t.Errorf("page still mentions %q", banned)
		}
	}
}

func TestPageWithNoCorrectionAndNoDangerZone(t *testing.T) {
	doc := syntheticDoc(t)
	r := doc.Runs[0]
	r.Correction = nil
	r.Warnings = []string{"only 40 answers in the fit half (need 50); no correction fitted"}
	r.DangerZone = analysis.Danger{Threshold: 0.95, N: 0, HitRate: analysis.Num(math.NaN()), CI95: [2]analysis.Num{analysis.Num(math.NaN()), analysis.Num(math.NaN())}}
	doc.Runs = []analysis.Run{r}
	page := render(t, doc)
	for _, want := range []string{"No correction was fitted: only 40 answers", "No answer reached 95.0% confidence."} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(page, "NaN") || strings.Contains(page, "+Inf") {
		t.Error("an undefined number leaked onto the page")
	}
}

func TestPageNamesTheReasonForAMissingCorrection(t *testing.T) {
	doc := syntheticDoc(t)
	for i := range doc.Runs {
		doc.Runs[i].Correction = nil
	}
	if page := render(t, doc); !strings.Contains(page, "No correction was fitted.") {
		t.Error("with no explaining warning the page should still say there is no correction")
	}
}

func TestPageEscapesNamesFromTheFile(t *testing.T) {
	doc := syntheticDoc(t)
	for i := range doc.Runs {
		doc.Runs[i].Model = `<img src=x onerror=alert(1)>` + doc.Runs[i].Model
	}
	out, err := Render(doc, Options{Title: `<b>x</b>`, Note: `<script>1</script>`})
	if err != nil {
		t.Fatal(err)
	}
	page := string(out)
	if strings.Contains(page, "<img src=x") || strings.Contains(page, "<script>1") || strings.Contains(page, "<b>x</b>") {
		t.Error("markup from the file reached the page unescaped")
	}
}

func TestRenderRefusesAnInconsistentDocument(t *testing.T) {
	good := func() analysis.Doc { return syntheticDoc(t) }
	cases := map[string]struct {
		mutate func(*analysis.Doc)
		want   string
	}{
		"no runs":        {func(d *analysis.Doc) { d.Runs = nil }, "no runs"},
		"duplicate run":  {func(d *analysis.Doc) { d.Runs = append(d.Runs, d.Runs[0]) }, "appears twice"},
		"bands disagree": {func(d *analysis.Doc) { d.Runs[1].BandsFine = d.Runs[1].BandsFine[:3] }, "inconsistent"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			doc := good()
			c.mutate(&doc)
			if _, err := Render(doc, Options{}); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestDefaultTitle(t *testing.T) {
	out, err := Render(syntheticDoc(t), Options{})
	if err != nil || !strings.Contains(string(out), "<title>BS Meter report</title>") {
		t.Fatalf("err %v; default title missing", err)
	}
}

func records(t *testing.T, write func(*bytes.Buffer) error) [][]string {
	t.Helper()
	var buf bytes.Buffer
	if err := write(&buf); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("csv does not parse: %v", err)
	}
	return rows
}

func TestRunsCSV(t *testing.T) {
	doc := syntheticDoc(t)
	rows := records(t, func(b *bytes.Buffer) error { return WriteRunsCSV(b, doc) })
	if len(rows) != len(doc.Runs)+1 {
		t.Fatalf("rows = %d, want header plus %d", len(rows), len(doc.Runs))
	}
	for i, r := range rows {
		if len(r) != len(rows[0]) {
			t.Fatalf("row %d has %d cells, header has %d", i, len(r), len(rows[0]))
		}
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		col[h] = i
	}
	first := rows[1]
	if first[col["model"]] != doc.Runs[0].Model || first[col["verdict"]] != VerdictFor(doc.Runs[0]).Key {
		t.Errorf("first row = %v", first)
	}
	if got := first[col["ece"]]; got == "" || got == "NaN" {
		t.Errorf("ece cell = %q", got)
	}
}

func TestCSVWritesNullAsAnEmptyCell(t *testing.T) {
	doc := syntheticDoc(t)
	doc.Runs[0].Correction = nil
	doc.Runs[0].DangerZone.HitRate = analysis.Num(math.NaN())
	rows := records(t, func(b *bytes.Buffer) error { return WriteRunsCSV(b, doc) })
	col := map[string]int{}
	for i, h := range rows[0] {
		col[h] = i
	}
	if rows[1][col["correction_t"]] != "" || rows[1][col["danger_hit_rate"]] != "" {
		t.Errorf("undefined values must be empty cells: %v", rows[1])
	}
}
