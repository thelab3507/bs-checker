package report

import (
	"fmt"
	"math"

	"github.com/thelab3507/bs-checker/internal/analysis"
)

// Verdict keys. They are part of the page's markup and the CSV, so keep them stable.
const (
	KeyOver   = "overconfident"
	KeyUnder  = "underconfident"
	KeyHonest = "honest"
	KeyFew    = "too-few"
)

// Verdict is the plain-English label for one run.
type Verdict struct {
	Key    string
	Label  string
	Detail string
}

func num(n analysis.Num) float64 { return float64(n) }

func missing(n analysis.Num) bool { return math.IsNaN(num(n)) }

// tooFew reports whether a run is too small to judge: the same thresholds that
// make the analysis warn.
func tooFew(r analysis.Run) bool {
	return r.N < analysis.MinAnswers || r.NClusters < analysis.MinClusters
}

// VerdictFor labels a run from the interval of its signed gap (mean confidence
// minus accuracy), a rule fixed before any real data was seen: an interval wholly
// above 0 is overconfident, wholly below 0 underconfident, spanning 0 honest.
// There is no size threshold, so the size is always printed beside the label.
func VerdictFor(r analysis.Run) Verdict {
	if tooFew(r) || missing(r.Gap.Value) || missing(r.Gap.CI95[0]) || missing(r.Gap.CI95[1]) {
		return Verdict{KeyFew, "Too few answers to judge",
			fmt.Sprintf("%d answers in %d independent groups; at least %d answers in %d groups are needed.", r.N, r.NClusters, analysis.MinAnswers, analysis.MinClusters)}
	}
	size := fmt.Sprintf("gap %s points, 95%% interval %s to %s", signed(num(r.Gap.Value)*100), signed(num(r.Gap.CI95[0])*100), signed(num(r.Gap.CI95[1])*100))
	switch lo, hi := num(r.Gap.CI95[0]), num(r.Gap.CI95[1]); {
	case lo > 0:
		return Verdict{KeyOver, "Overconfident",
			fmt.Sprintf("On average it claims more than it delivers (%s).", size)}
	case hi < 0:
		return Verdict{KeyUnder, "Underconfident",
			fmt.Sprintf("On average it claims less than it delivers (%s).", size)}
	}
	return Verdict{KeyHonest, "Honest",
		fmt.Sprintf("Average confidence matches accuracy within the error bars (%s). Bands can still be off in opposite directions; see the chart.", size)}
}

// signed formats a number with an explicit sign and one decimal, using a true
// minus sign. A value that rounds to zero carries no sign.
func signed(v float64) string {
	if math.IsNaN(v) {
		return "n/a"
	}
	s := fmt.Sprintf("%.1f", math.Abs(v))
	if s == "0.0" {
		// A small but real difference: give it a second decimal rather than a
		// misleading flat zero.
		s = fmt.Sprintf("%.2f", math.Abs(v))
	}
	switch {
	case s == "0.0" || s == "0.00":
		return "0.0"
	case v < 0:
		return "−" + s
	}
	return "+" + s
}

// pct formats a 0..1 rate as a percentage with one decimal.
func pct(n analysis.Num) string {
	if missing(n) {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", num(n)*100)
}

// pts formats a 0..1 value as unsigned points; used for ECE, which is never negative.
func pts(n analysis.Num) string {
	if missing(n) {
		return "n/a"
	}
	return fmt.Sprintf("%.1f", num(n)*100)
}

// dec formats a raw score such as Brier or log loss.
func dec(n analysis.Num) string {
	if missing(n) {
		return "n/a"
	}
	return fmt.Sprintf("%.3f", num(n))
}

func signedPts(n analysis.Num) string {
	if missing(n) {
		return "n/a"
	}
	return signed(num(n) * 100)
}
