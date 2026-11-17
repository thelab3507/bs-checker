// Package report renders an analysis document as one self-contained HTML page:
// inline SVG charts, an embedded font, no scripts and no external requests.
// The same document always gives the same bytes.
package report

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"math"
	"slices"
	"strings"

	"github.com/thelab3507/bs-checker/internal/analysis"
)

//go:embed template.html
var pageTemplate string

//go:embed assets/plus-jakarta-sans-latin-wght-normal.woff2
var fontFile []byte

// Options are the two pieces of text the analysis does not carry.
type Options struct {
	// Title is the page heading and <title>.
	Title string
	// Note is an optional line under the heading, such as "Synthetic data".
	Note string
}

type kv struct{ Key, Value string }

type metricRow struct{ Name, Value, Interval, Help string }

type bandRow struct{ Range, N, Mean, Hit string }

type bandTable struct {
	Title string
	Rows  []bandRow
}

type dangerView struct {
	Sentence string
	Chart    template.HTML
}

type correctionRow struct{ Name, Before, After, Change string }

type correctionView struct {
	Rows   []correctionRow
	Method string
	Detail string
	Why    string // set when there is no correction
}

type runView struct {
	ID         string
	Model      string
	Quant      string
	Host       string
	Method     string
	Set        string
	Class      string
	Verdict    Verdict
	Metrics    []metricRow
	Chart      template.HTML
	Tables     []bandTable
	Danger     dangerView
	Correction correctionView
	Warnings   []string
}

// answerView is one line of "The answer": the verdict on one run's confidence,
// one line per model, question set and quantisation.
type answerView struct {
	Where string
	Link  string
	Quant string
	Class string
	Verdict
}

type blockView struct {
	Anchor              string
	Model, Host, Method string
	Runs                []runView
}

type setView struct {
	Name   string
	Blocks []blockView
}

type page struct {
	Title, Note    string
	Seed           uint64
	Resamples      int
	BandScheme     string
	FineBandScheme string
	ClusterBy      []kv
	Warnings       []string
	Answers        []answerView
	Sets           []setView
	FontCSS        template.CSS
}

var tmpl = template.Must(template.New("report").Parse(pageTemplate))

// Render builds the page. It refuses a document that cannot be drawn honestly:
// no runs, or runs whose bands disagree.
func Render(doc analysis.Doc, o Options) ([]byte, error) {
	p, err := buildPage(doc, o)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, p); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type runID struct{ model, quant, host, method, set string }

func idOf(r analysis.Run) runID { return runID{r.Model, r.Quant, r.Host, r.Method, r.Set} }

func validate(doc analysis.Doc) error {
	if len(doc.Runs) == 0 {
		return fmt.Errorf("the analysis holds no runs, so there is nothing to report")
	}
	seen := map[runID]bool{}
	for _, r := range doc.Runs {
		if seen[idOf(r)] {
			return fmt.Errorf("run %s %s %s %s appears twice", r.Model, r.Quant, r.Host, r.Set)
		}
		seen[idOf(r)] = true
		if len(r.BandsFine) == 0 || len(r.BandsFine) != len(doc.Runs[0].BandsFine) || len(r.Bands) == 0 {
			return fmt.Errorf("run %s %s %s %s has %d fine bands, the first run has %d; the analysis files are inconsistent",
				r.Model, r.Quant, r.Host, r.Set, len(r.BandsFine), len(doc.Runs[0].BandsFine))
		}
	}
	return nil
}

// slots gives every quantisation one colour for the whole page, in name order.
func slots(doc analysis.Doc) map[string]int {
	var quants []string
	for _, r := range doc.Runs {
		quants = append(quants, r.Quant)
	}
	slices.Sort(quants)
	m := map[string]int{}
	for _, q := range quants {
		if _, ok := m[q]; !ok {
			m[q] = len(m) + 1
		}
	}
	return m
}

func buildPage(doc analysis.Doc, o Options) (page, error) {
	if err := validate(doc); err != nil {
		return page{}, err
	}
	slotOf := slots(doc)
	title := o.Title
	if title == "" {
		title = "BS Meter report"
	}
	p := page{
		Title: title, Note: o.Note, Seed: doc.Seed, Resamples: doc.BootstrapResamples,
		BandScheme: doc.BandScheme, FineBandScheme: doc.FineBandScheme, Warnings: doc.Warnings,
		FontCSS: template.CSS(`@font-face{font-family:"Plus Jakarta Sans Variable";font-style:normal;font-display:swap;font-weight:200 800;src:url(data:font/woff2;base64,` +
			base64.StdEncoding.EncodeToString(fontFile) + `) format("woff2")}`),
	}
	for set, rule := range doc.ClusterBy {
		p.ClusterBy = append(p.ClusterBy, kv{set, rule})
	}
	slices.SortFunc(p.ClusterBy, func(a, b kv) int { return strings.Compare(a.Key, b.Key) })

	sets := map[string]map[[3]string]bool{}
	for _, r := range doc.Runs {
		if sets[r.Set] == nil {
			sets[r.Set] = map[[3]string]bool{}
		}
		sets[r.Set][[3]string{r.Model, r.Host, r.Method}] = true
	}
	setNames := make([]string, 0, len(sets))
	for s := range sets {
		setNames = append(setNames, s)
	}
	slices.Sort(setNames)

	runNo := 0
	for _, set := range setNames {
		keys := make([][3]string, 0, len(sets[set]))
		for k := range sets[set] {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, func(a, b [3]string) int { return strings.Compare(strings.Join(a[:], "|"), strings.Join(b[:], "|")) })
		sv := setView{Name: set}
		for _, k := range keys {
			bv := blockView{Model: k[0], Host: k[1], Method: k[2]}
			var runs []analysis.Run
			for _, r := range doc.Runs {
				if r.Set == set && r.Model == k[0] && r.Host == k[1] && r.Method == k[2] {
					runs = append(runs, r)
				}
			}
			slices.SortStableFunc(runs, func(a, b analysis.Run) int {
				if c := slotOf[a.Quant] - slotOf[b.Quant]; c != 0 {
					return c
				}
				return strings.Compare(a.Quant, b.Quant)
			})
			for _, r := range runs {
				runNo++
				bv.Runs = append(bv.Runs, runViewOf(r, fmt.Sprintf("run-%d", runNo), slotOf[r.Quant]))
			}
			bv.Anchor = "block-" + anchor(k, set)
			for _, rv := range bv.Runs {
				p.Answers = append(p.Answers, answerView{
					Where: fmt.Sprintf("%s · %s · %s (%s)", k[0], set, k[1], k[2]), Link: bv.Anchor,
					Quant: rv.Quant, Class: rv.Class, Verdict: rv.Verdict,
				})
			}
			sv.Blocks = append(sv.Blocks, bv)
		}
		p.Sets = append(p.Sets, sv)
	}
	return p, nil
}

func anchor(k [3]string, set string) string {
	return strings.NewReplacer(" ", "-", "/", "-", ":", "-", "|", "-").Replace(strings.Join([]string{set, k[0], k[1], k[2]}, "-"))
}

func interval(m analysis.Measure, f func(analysis.Num) string) string {
	if missing(m.CI95[0]) || missing(m.CI95[1]) {
		return "n/a"
	}
	return f(m.CI95[0]) + " to " + f(m.CI95[1])
}

func runViewOf(r analysis.Run, id string, slot int) runView {
	rv := runView{
		ID: id, Model: r.Model, Quant: r.Quant, Host: r.Host, Method: r.Method, Set: r.Set,
		Class: slotClass(slot), Verdict: VerdictFor(r), Warnings: r.Warnings,
	}
	rv.Metrics = []metricRow{
		{"Accuracy", pct(r.Accuracy.Value), interval(r.Accuracy, pct), "How often the answer was right."},
		{"ECE, 10 bands", pts(r.ECE.Value) + " points", interval(r.ECE, pts), "Average distance between how sure it said it was and how often it was right. 0 is perfect."},
		{"ECE, fine bands", pts(r.ECEFine.Value) + " points", interval(r.ECEFine, pts), "The same, with finer bands near 100%, where a saturated model sits."},
		{"Brier score", dec(r.Brier.Value), interval(r.Brier, dec), "Squared error of its confidence against what happened. Lower is better; needs no bands."},
		{"Log loss", dec(r.LogLoss.Value), interval(r.LogLoss, dec), "Punishes confident wrong answers heavily. Lower is better."},
		{"Gap", signedPts(r.Gap.Value) + " points", interval(r.Gap, signedPts), "Average confidence minus accuracy. Above 0 is overconfident, below 0 underconfident."},
	}
	rv.Chart = svgReliability(fmt.Sprintf("%s %s on %s: how often the model was right in each confidence band, and how sure it said it was", r.Model, r.Quant, r.Set),
		[]series{{r.Quant, slot, r.BandsFine}})
	rv.Tables = []bandTable{
		{"Fine bands (the chart)", bandRows(r.BandsFine)},
		{"10 equal bands (the headline ECE)", bandRows(r.Bands)},
	}

	d := r.DangerZone
	switch {
	case d.N == 0:
		rv.Danger.Sentence = fmt.Sprintf("No answer reached %s confidence.", pct(d.Threshold))
	case d.Wrong == 0:
		rv.Danger.Sentence = fmt.Sprintf("All %d answers given at %s confidence or more were right. With so few, the true error rate could still be a few percent.", d.N, pct(d.Threshold))
	default:
		rv.Danger.Sentence = fmt.Sprintf("Of %d answers given at %s confidence or more, %d were wrong: it was right %s of the time (95%% interval %s to %s).",
			d.N, pct(d.Threshold), d.Wrong, pct(d.HitRate), pct(d.CI95[0]), pct(d.CI95[1]))
	}
	rv.Danger.Chart = svgDanger(d, slot)

	if c := r.Correction; c != nil {
		rv.Correction = correctionView{
			Method: c.Method,
			Rows: []correctionRow{
				{"ECE, 10 bands", pts(c.ECEBefore) + " points", pts(c.ECEAfter) + " points", change(num(c.ECEBefore), num(c.ECEAfter))},
				{"Log loss", dec(c.NLLBefore), dec(c.NLLAfter), change(num(c.NLLBefore), num(c.NLLAfter))},
			},
		}
		t := num(c.Params["T"])
		how := "softens its confidence"
		if t < 1 {
			how = "sharpens its confidence"
		}
		rv.Correction.Detail = fmt.Sprintf("Temperature T = %.2f, which %s. Fitted on %d answers from one random half of the groups and scored on the other %d, which it had not seen. Accuracy is unchanged: this rescales confidence and never changes an answer.", t, how, c.FitN, c.EvalN)
	} else {
		rv.Correction.Why = "No correction was fitted."
		for _, w := range r.Warnings {
			if strings.Contains(w, "no correction") {
				rv.Correction.Why = "No correction was fitted: " + w + "."
			}
		}
	}
	return rv
}

func change(before, after float64) string {
	switch {
	case math.IsNaN(before) || math.IsNaN(after):
		return "n/a"
	case math.Abs(after-before) < 5e-4:
		return "about the same"
	case after < before:
		return "better"
	}
	return "worse"
}

func bandRows(bs []analysis.Band) []bandRow {
	rows := make([]bandRow, len(bs))
	for i, b := range bs {
		rows[i] = bandRow{Range: bandLabel(b), N: fmt.Sprint(b.N), Mean: "–", Hit: "–"}
		if bandUsed(b) {
			rows[i].Mean, rows[i].Hit = pct(b.MeanConf), pct(b.HitRate)
		}
	}
	return rows
}
