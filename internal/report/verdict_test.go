package report

import (
	"math"
	"strings"
	"testing"

	"github.com/thelab3507/bs-checker/internal/analysis"
)

func measure(v, lo, hi float64) analysis.Measure {
	return analysis.Measure{Value: analysis.Num(v), CI95: [2]analysis.Num{analysis.Num(lo), analysis.Num(hi)}}
}

func runWithGap(quant string, n, clusters int, v, lo, hi float64) analysis.Run {
	return analysis.Run{Model: "m", Quant: quant, N: n, NClusters: clusters, Gap: measure(v, lo, hi)}
}

func TestVerdictFollowsTheGapInterval(t *testing.T) {
	cases := []struct {
		name string
		run  analysis.Run
		want string
	}{
		{"interval above zero is overconfident", runWithGap("Q4", 400, 400, 0.05, 0.02, 0.08), KeyOver},
		{"interval below zero is underconfident", runWithGap("Q4", 400, 400, -0.05, -0.08, -0.02), KeyUnder},
		{"interval spanning zero is honest", runWithGap("Q4", 400, 400, 0.01, -0.02, 0.04), KeyHonest},
		{"an interval touching zero is not significant", runWithGap("Q4", 400, 400, 0.02, 0, 0.04), KeyHonest},
		{"too few answers beat any gap", runWithGap("Q4", 99, 99, 0.2, 0.15, 0.25), KeyFew},
		{"too few groups beat any gap", runWithGap("Q4", 500, 18, 0.2, 0.15, 0.25), KeyFew},
		{"an undefined gap cannot be judged", runWithGap("Q4", 400, 400, math.NaN(), math.NaN(), math.NaN()), KeyFew},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := VerdictFor(c.run).Key; got != c.want {
				t.Fatalf("verdict = %s, want %s", got, c.want)
			}
		})
	}
}

func TestVerdictPrintsTheSizeBesideTheLabel(t *testing.T) {
	v := VerdictFor(runWithGap("Q4", 400, 400, 0.0412, 0.02, 0.0623))
	for _, want := range []string{"+4.1", "+2.0", "+6.2"} {
		if !strings.Contains(v.Detail, want) {
			t.Errorf("detail %q lacks %q", v.Detail, want)
		}
	}
	if f := VerdictFor(runWithGap("Q4", 40, 12, 0.1, 0.0, 0.2)); !strings.Contains(f.Detail, "40 answers") || !strings.Contains(f.Detail, "12 independent groups") {
		t.Errorf("too-few detail = %q", f.Detail)
	}
}

func TestNumberFormatting(t *testing.T) {
	cases := []struct{ got, want string }{
		{signed(4.12), "+4.1"},
		{signed(-0.9), "−0.9"},
		{signed(0), "0.0"},
		{signed(0.03), "+0.03"},
		{signed(-0.03), "−0.03"},
		{signed(0.0004), "0.0"},
		{signed(math.NaN()), "n/a"},
		{pct(0.99991), "100.0%"},
		{pct(analysis.Num(math.NaN())), "n/a"},
		{pts(0.0366), "3.7"},
		{dec(0.1304), "0.130"},
		{signedPts(analysis.Num(math.NaN())), "n/a"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
