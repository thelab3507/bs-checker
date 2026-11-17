// Package analysis turns a results file into the analysis document: per-run
// accuracy, reliability bands, ECE, scores, danger zone and temperature
// correction, all with bootstrap intervals.
package analysis

import (
	"cmp"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/thelab3507/bs-checker/internal/calib"
	"github.com/thelab3507/bs-checker/internal/results"
)

const (
	minFitAnswers  = 50
	MinClusters    = 30
	MinAnswers     = 100
	lowMassBelow   = 0.9
	roundDecimals  = 1e9
	headlineBands  = 10
	correctionName = "temperature"
)

// DefaultClusterRule groups fresh-news questions by story: ids look like
// news-<story>-<nn>, and questions about one story share a passage.
const DefaultClusterRule = `fresh-news=^news-(.+)-\d+$`

type ClusterRule struct {
	Set string
	Re  *regexp.Regexp
}

// ParseClusterRule reads "set=regexp"; the regexp's first group is the cluster key.
func ParseClusterRule(s string) (ClusterRule, error) {
	set, expr, ok := strings.Cut(s, "=")
	if !ok || set == "" || expr == "" {
		return ClusterRule{}, fmt.Errorf("cluster rule %q: want set=regexp", s)
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return ClusterRule{}, fmt.Errorf("cluster rule %q: %w", s, err)
	}
	if re.NumSubexp() < 1 {
		return ClusterRule{}, fmt.Errorf("cluster rule %q: the regexp needs a capture group for the cluster key", s)
	}
	return ClusterRule{Set: set, Re: re}, nil
}

type Options struct {
	Seed      uint64
	Resamples int
	Danger    float64
	Rules     []ClusterRule
}

// Num is a float written with fixed rounding; NaN and Inf become null.
type Num float64

func (n Num) MarshalJSON() ([]byte, error) {
	f := float64(n)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return []byte("null"), nil
	}
	return strconv.AppendFloat(nil, math.Round(f*roundDecimals)/roundDecimals, 'g', -1, 64), nil
}

// UnmarshalJSON reads null back as NaN, so an empty band or danger zone is not
// mistaken for a measured zero.
func (n *Num) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*n = Num(math.NaN())
		return nil
	}
	f, err := strconv.ParseFloat(string(data), 64)
	if err != nil {
		return fmt.Errorf("want a number or null, got %s", data)
	}
	*n = Num(f)
	return nil
}

type Measure struct {
	Value Num    `json:"value"`
	CI95  [2]Num `json:"ci95"`
}

type Band struct {
	Lo       Num `json:"lo"`
	Hi       Num `json:"hi"`
	N        int `json:"n"`
	MeanConf Num `json:"mean_conf"`
	HitRate  Num `json:"hit_rate"`
}

type Danger struct {
	Threshold Num    `json:"threshold"`
	N         int    `json:"n"`
	Wrong     int    `json:"wrong"`
	HitRate   Num    `json:"hit_rate"`
	CI95      [2]Num `json:"ci95"`
}

type Correction struct {
	Method    string         `json:"method"`
	Params    map[string]Num `json:"params"`
	FitN      int            `json:"fit_n"`
	EvalN     int            `json:"eval_n"`
	ECEBefore Num            `json:"ece_before"`
	ECEAfter  Num            `json:"ece_after"`
	NLLBefore Num            `json:"nll_before"`
	NLLAfter  Num            `json:"nll_after"`
}

type Run struct {
	Model      string      `json:"model"`
	Quant      string      `json:"quant"`
	Host       string      `json:"host"`
	Method     string      `json:"method"`
	Set        string      `json:"set"`
	N          int         `json:"n"`
	NClusters  int         `json:"n_clusters"`
	Accuracy   Measure     `json:"accuracy"`
	ECE        Measure     `json:"ece"`
	ECEFine    Measure     `json:"ece_fine"`
	Brier      Measure     `json:"brier"`
	LogLoss    Measure     `json:"log_loss"`
	Gap        Measure     `json:"gap"`
	Bands      []Band      `json:"bands"`
	BandsFine  []Band      `json:"bands_fine"`
	DangerZone Danger      `json:"danger_zone"`
	Correction *Correction `json:"correction"`
	LowMassN   int         `json:"low_mass_n"`
	Warnings   []string    `json:"warnings"`
}

type Doc struct {
	FormatVersion      int               `json:"format_version"`
	Seed               uint64            `json:"seed"`
	BootstrapResamples int               `json:"bootstrap_resamples"`
	BandScheme         string            `json:"band_scheme"`
	FineBandScheme     string            `json:"fine_band_scheme"`
	ClusterBy          map[string]string `json:"cluster_by"`
	Runs               []Run             `json:"runs"`
	Warnings           []string          `json:"warnings"`
}

type runKey struct{ model, quant, host, method, set string }

func (k runKey) String() string {
	return strings.Join([]string{k.model, k.quant, k.host, k.method, k.set}, "|")
}

// rngFor gives every run (and purpose within it) its own stream, so adding or
// reordering runs never changes another run's numbers.
func rngFor(seed uint64, name string) *rand.Rand {
	h := fnv.New64a()
	h.Write([]byte(name))
	return rand.New(rand.NewPCG(seed, h.Sum64()))
}

func Build(rows []results.Result, o Options) (Doc, error) {
	if len(rows) == 0 {
		return Doc{}, fmt.Errorf("no results to analyse")
	}
	if o.Resamples < 100 {
		return Doc{}, fmt.Errorf("-resamples %d is too few for a 95%% interval, want at least 100", o.Resamples)
	}
	groups := map[runKey][]results.Result{}
	sets := map[string]bool{}
	for _, r := range rows {
		k := runKey{r.Model, r.Quant, r.Host, r.Method, r.Set}
		groups[k] = append(groups[k], r)
		sets[r.Set] = true
	}
	keys := make([]runKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b runKey) int { return cmp.Compare(a.String(), b.String()) })

	doc := Doc{
		FormatVersion: 1, Seed: o.Seed, BootstrapResamples: o.Resamples,
		BandScheme:     fmt.Sprintf("%d equal-width bands over [0.5, 1.0]", headlineBands),
		FineBandScheme: fmt.Sprintf("%d bands with cut points %s", len(calib.FineEdges)-1, joinFloats(calib.FineEdges)),
		ClusterBy:      map[string]string{},
		Runs:           []Run{}, Warnings: []string{},
	}
	for set := range sets {
		doc.ClusterBy[set] = "qid"
		if re := ruleFor(o.Rules, set); re != nil {
			doc.ClusterBy[set] = "regexp " + re.String()
		}
	}

	for _, k := range keys {
		re := ruleFor(o.Rules, k.set)
		obs := make([]calib.Obs, len(groups[k]))
		for i, r := range groups[k] {
			obs[i] = calib.Obs{Conf: r.PChosen, Correct: r.Correct, Cluster: clusterOf(re, r.QID)}
		}
		doc.Runs = append(doc.Runs, analyseRun(k, groups[k], obs, o))
	}
	return doc, nil
}

func ruleFor(rules []ClusterRule, set string) *regexp.Regexp {
	for _, r := range rules {
		if r.Set == set {
			return r.Re
		}
	}
	return nil
}

func clusterOf(re *regexp.Regexp, qid string) string {
	if re != nil {
		if m := re.FindStringSubmatch(qid); m != nil {
			return m[1]
		}
	}
	return qid
}

func joinFloats(f []float64) string {
	s := make([]string, len(f))
	for i, v := range f {
		s[i] = strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strings.Join(s, ", ")
}

var (
	headlineEdges = calib.EqualEdges(0.5, 1.0, headlineBands)
)

func analyseRun(k runKey, rows []results.Result, obs []calib.Obs, o Options) Run {
	name := k.String()
	ci := rngFor(o.Seed, name+"|ci")
	measure := func(stat func([]calib.Obs) float64) Measure {
		lo, hi := calib.Bootstrap(obs, o.Resamples, ci, stat)
		return Measure{Value: Num(stat(obs)), CI95: [2]Num{Num(lo), Num(hi)}}
	}
	ece := func(edges []float64) func([]calib.Obs) float64 {
		return func(x []calib.Obs) float64 { return calib.ECE(x, edges) }
	}

	_, clusters := calib.GroupByCluster(obs)
	r := Run{
		Model: k.model, Quant: k.quant, Host: k.host, Method: k.method, Set: k.set,
		N: len(obs), NClusters: len(clusters),
		Accuracy:  measure(calib.Accuracy),
		ECE:       measure(ece(headlineEdges)),
		ECEFine:   measure(ece(calib.FineEdges)),
		Brier:     measure(calib.Brier),
		LogLoss:   measure(calib.LogLoss),
		Gap:       measure(calib.Gap),
		Bands:     bands(obs, headlineEdges),
		BandsFine: bands(obs, calib.FineEdges),
		Warnings:  []string{},
	}

	dn, dhit, ok := calib.DangerZone(obs, o.Danger)
	r.DangerZone = Danger{Threshold: Num(o.Danger), N: dn, HitRate: Num(math.NaN())}
	if ok {
		r.DangerZone.HitRate = Num(dhit)
		r.DangerZone.Wrong = dn - int(math.Round(dhit*float64(dn)))
	}
	lo, hi := calib.Bootstrap(obs, o.Resamples, ci, func(x []calib.Obs) float64 {
		_, h, ok := calib.DangerZone(x, o.Danger)
		if !ok {
			return math.NaN()
		}
		return h
	})
	r.DangerZone.CI95 = [2]Num{Num(lo), Num(hi)}

	for _, row := range rows {
		if row.AnswerMass < lowMassBelow {
			r.LowMassN++
		}
	}
	if r.LowMassN > 0 {
		r.Warnings = append(r.Warnings, fmt.Sprintf("%d answers put under %.0f%% of their probability on the answer tokens; the model may not be answering Yes/No", r.LowMassN, 100*lowMassBelow))
	}
	if r.N < MinAnswers {
		r.Warnings = append(r.Warnings, fmt.Sprintf("only %d answers; bands are sparse and intervals wide", r.N))
	}
	if r.NClusters < MinClusters {
		r.Warnings = append(r.Warnings, fmt.Sprintf("only %d clusters; bootstrap intervals are approximate", r.NClusters))
	}

	if r.DangerZone.N > 0 && r.DangerZone.Wrong == 0 {
		r.Warnings = append(r.Warnings, fmt.Sprintf("no wrong answers among the %d at %.0f%% or more; the interval collapses to 1, but the true error rate could still be about %.1f%% (rule of three)", r.DangerZone.N, 100*o.Danger, 300/float64(r.DangerZone.N)))
	}

	fit, eval := calib.SplitClusters(obs, rngFor(o.Seed, name+"|split"))
	switch {
	case len(fit) < minFitAnswers || len(eval) == 0:
		r.Warnings = append(r.Warnings, fmt.Sprintf("only %d answers in the fit half (need %d); no correction fitted", len(fit), minFitAnswers))
	default:
		t, atBound := calib.FitTemperature(fit)
		if atBound {
			r.Warnings = append(r.Warnings, fmt.Sprintf("temperature fit reached its search bound (T=%.3g); the fit half cannot pin it down, so no correction is offered", t))
			break
		}
		after := calib.ApplyTemperature(eval, t)
		r.Correction = &Correction{
			Method: correctionName, Params: map[string]Num{"T": Num(t)}, FitN: len(fit), EvalN: len(eval),
			ECEBefore: Num(calib.ECE(eval, headlineEdges)), ECEAfter: Num(calib.ECE(after, headlineEdges)),
			NLLBefore: Num(calib.LogLoss(eval)), NLLAfter: Num(calib.LogLoss(after)),
		}
	}
	return r
}

func bands(obs []calib.Obs, edges []float64) []Band {
	out := []Band{}
	for _, b := range calib.Bands(obs, edges) {
		nb := Band{Lo: Num(b.Lo), Hi: Num(b.Hi), N: b.N, MeanConf: Num(math.NaN()), HitRate: Num(math.NaN())}
		if b.N > 0 {
			nb.MeanConf, nb.HitRate = Num(b.MeanConf), Num(b.HitRate)
		}
		out = append(out, nb)
	}
	return out
}

// CorrectionFile is what other tools read: the method and its parameters.
type CorrectionFile struct {
	Method string         `json:"method"`
	Params map[string]Num `json:"params"`
}

// CorrectionName is the file name for a run's saved correction.
func CorrectionName(r Run) string {
	clean := func(s string) string { return strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(s) }
	return fmt.Sprintf("%s-%s-%s-%s.json", clean(r.Model), clean(r.Quant), clean(r.Host), clean(r.Set))
}

// Read loads an analysis file written by WriteFile. It refuses a file from a
// different format version, or one written before the signed gap existed, so a
// stale analysis.json fails loudly instead of rendering blanks.
func Read(path string) (Doc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Doc{}, err
	}
	var doc Doc
	if err := json.Unmarshal(data, &doc); err != nil {
		return Doc{}, fmt.Errorf("%s: %w", path, err)
	}
	if doc.FormatVersion != 1 {
		return Doc{}, fmt.Errorf("%s: format_version %d, this bsmeter reads 1", path, doc.FormatVersion)
	}
	var raw struct {
		Runs []map[string]json.RawMessage `json:"runs"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Doc{}, fmt.Errorf("%s: %w", path, err)
	}
	for i, r := range raw.Runs {
		if _, ok := r["gap"]; !ok {
			return Doc{}, fmt.Errorf("%s: run %d has no \"gap\"; the file predates it, rerun bsmeter analyze", path, i+1)
		}
	}
	return doc, nil
}

func Marshal(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func WriteFile(path string, v any) error {
	data, err := Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
