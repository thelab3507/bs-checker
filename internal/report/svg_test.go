package report

import (
	"encoding/xml"
	"html/template"
	"io"
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/thelab3507/bs-checker/internal/analysis"
)

func band(lo, hi float64, n int, mean, hit float64) analysis.Band {
	b := analysis.Band{Lo: analysis.Num(lo), Hi: analysis.Num(hi), N: n, MeanConf: analysis.Num(math.NaN()), HitRate: analysis.Num(math.NaN())}
	if n > 0 {
		b.MeanConf, b.HitRate = analysis.Num(mean), analysis.Num(hit)
	}
	return b
}

// wellFormed fails unless the SVG parses as XML, and returns its element counts.
func wellFormed(t *testing.T, svg template.HTML) map[string]int {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(string(svg)))
	count := map[string]int{}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return count
		}
		if err != nil {
			t.Fatalf("not well-formed XML: %v\n%s", err, svg)
		}
		if se, ok := tok.(xml.StartElement); ok {
			count[se.Name.Local]++
		}
	}
}

func fineBands() []analysis.Band {
	return []analysis.Band{
		band(0.5, 0.6, 0, 0, 0), band(0.6, 0.7, 30, 0.65, 0.5), band(0.7, 0.8, 40, 0.75, 0.75),
		band(0.8, 0.9, 5, 0.85, 0.8), band(0.9, 0.95, 0, 0, 0), band(0.95, 0.99, 0, 0, 0),
		band(0.99, 0.999, 0, 0, 0), band(0.999, 0.9999, 0, 0, 0), band(0.9999, 1, 100, 0.99995, 0.97),
	}
}

func TestReliabilityDrawsOneMarkPerUsedBand(t *testing.T) {
	svg := svgReliability("t", []series{{"Q4", 1, fineBands()}})
	n := wellFormed(t, svg)
	// Four used bands: each gets a dot (circle), a tick and a gap line; the
	// legend swatch for the series is one more circle.
	if n["circle"] != 4+1 {
		t.Errorf("circles = %d, want 4 dots and 1 swatch", n["circle"])
	}
	if got := strings.Count(string(svg), `class="tick"`); got != 4 {
		t.Errorf("ticks = %d, want 4", got)
	}
	if got := strings.Count(string(svg), "hollow"); got != 1 {
		t.Errorf("hollow dots = %d, want 1 (the band with 5 answers)", got)
	}
	if !strings.Contains(string(svg), `>0.99<`) && !strings.Contains(string(svg), `>0.9999<`) {
		t.Error("band edges are missing from the axis")
	}
}

func TestReliabilityEmptyBandsDrawNothingButANumberDash(t *testing.T) {
	bands := fineBands()
	svg := string(svgReliability("t", []series{{"Q4", 1, bands}}))
	if got := strings.Count(svg, ">–<"); got != 5 {
		t.Errorf("empty-band dashes = %d, want 5", got)
	}
}

func TestReliabilityTwoSeriesAreOffsetAndLabelled(t *testing.T) {
	a, b := fineBands(), fineBands()
	svg := svgReliability("t", []series{{"Q4", 1, a}, {"Q8", 2, b}})
	n := wellFormed(t, svg)
	if n["circle"] != 8+2 {
		t.Errorf("circles = %d, want 8 dots and 2 swatches", n["circle"])
	}
	for _, class := range []string{"fill-s1", "fill-s2"} {
		if !strings.Contains(string(svg), class) {
			t.Errorf("missing %s", class)
		}
	}
	xs := regexp.MustCompile(`class="dot fill-s1" cx="([\d.]+)"`).FindStringSubmatch(string(svg))
	ys := regexp.MustCompile(`class="dot fill-s2" cx="([\d.]+)"`).FindStringSubmatch(string(svg))
	if xs == nil || ys == nil || xs[1] == ys[1] {
		t.Errorf("the two series' dots must not sit on the same x: %v %v", xs, ys)
	}
}

func TestReliabilityIsAnnotatedForAssistiveTech(t *testing.T) {
	svg := string(svgReliability(`How often "Q4" was right`, []series{{"Q4", 1, fineBands()}}))
	if !strings.Contains(svg, `role="img"`) || !strings.Contains(svg, `aria-label="How often &#34;Q4&#34; was right"`) || !strings.Contains(svg, "<title>") {
		t.Errorf("chart lacks an accessible name: %s", svg[:200])
	}
}

func TestReliabilityAxisCoversTheDataAndEndsAt100(t *testing.T) {
	svg := string(svgReliability("t", []series{{"Q4", 1, []analysis.Band{band(0.5, 0.6, 50, 0.55, 0.12), band(0.6, 1, 50, 0.9, 0.9)}}}))
	if !strings.Contains(svg, ">100%<") || !strings.Contains(svg, ">20%<") {
		t.Errorf("axis should run from the lowest hit rate (10%% floor) up to 100%%: %s", regexp.MustCompile(`>\d+%<`).FindAllString(svg, -1))
	}
}

func TestEverythingTextualIsEscaped(t *testing.T) {
	evil := `<script>alert(1)</script>`
	svgs := []template.HTML{
		svgReliability(evil, []series{{evil, 1, fineBands()}}),
		svgDanger(analysis.Danger{Threshold: 0.95, N: 10, Wrong: 1, HitRate: 0.9, CI95: [2]analysis.Num{0.7, 0.99}}, 1),
	}
	for _, svg := range svgs {
		wellFormed(t, svg)
		s := string(svg)
		if strings.Contains(s, "<script") || strings.Contains(s, "alert(1)</") {
			t.Errorf("unescaped text in %s", s)
		}
		if strings.Contains(s, "http://") && strings.Count(s, "http://") != strings.Count(s, "http://www.w3.org/2000/svg") {
			t.Errorf("external reference in chart: %s", s)
		}
	}
}

func TestDangerSvg(t *testing.T) {
	d := analysis.Danger{Threshold: 0.95, N: 148, Wrong: 15, HitRate: 0.899, CI95: [2]analysis.Num{0.847, 0.948}}
	svg := svgDanger(d, 1)
	wellFormed(t, svg)
	if !strings.Contains(string(svg), "claim") || !strings.Contains(string(svg), "claimed 95.0% or more") {
		t.Errorf("the claimed line is missing: %s", svg)
	}
	if svgDanger(analysis.Danger{Threshold: 0.95, HitRate: analysis.Num(math.NaN())}, 1) != "" {
		t.Error("an empty danger zone draws nothing")
	}
	// No errors: the interval collapses onto 1; the chart must still draw.
	perfect := svgDanger(analysis.Danger{Threshold: 0.95, N: 30, HitRate: 1, CI95: [2]analysis.Num{1, 1}}, 2)
	wellFormed(t, perfect)
	if strings.Contains(string(perfect), "NaN") {
		t.Error("NaN leaked into the chart")
	}
}
