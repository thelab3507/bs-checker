package report

import (
	"fmt"
	"html"
	"html/template"
	"math"
	"strconv"
	"strings"

	"github.com/thelab3507/bs-checker/internal/analysis"
)

// Charts are plain SVG strings. Colours, fonts and strokes come from CSS classes
// in the page's stylesheet, so light and dark mode need nothing here. Every piece
// of text passes through esc.

func esc(s string) string { return html.EscapeString(s) }

func ff(v float64) string { return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64) }

// minBandN is the fewest answers a band needs for its dot to be drawn solid.
const minBandN = 10

// series is one run's bands, drawn in one categorical slot (1 to 8).
type series struct {
	Label string
	Slot  int
	Bands []analysis.Band
}

func slotClass(slot int) string {
	if slot < 1 || slot > 8 {
		slot = 8
	}
	return "s" + strconv.Itoa(slot)
}

// tickStep picks a round axis step for a span of 0..1 values.
func tickStep(span float64) float64 {
	switch {
	case span <= 0.3:
		return 0.05
	case span <= 0.6:
		return 0.1
	}
	return 0.2
}

func edge(v analysis.Num) string { return strconv.FormatFloat(num(v), 'g', -1, 64) }

// bandLabel is "0.9–0.95" for a band.
func bandLabel(b analysis.Band) string { return edge(b.Lo) + "–" + edge(b.Hi) }

func bandUsed(b analysis.Band) bool {
	return b.N > 0 && !missing(b.HitRate) && !missing(b.MeanConf)
}

const (
	relW     = 640
	relL     = 52.0
	relR     = 12.0
	relTop   = 16.0
	relPlotB = 196.0
)

// svgReliability draws one or two runs over the same bands. For each band a dot
// marks how often the model was right, a tick marks how sure it said it was, and
// a line joins them: a dot below its tick is overconfidence, above is
// underconfidence. A band with under minBandN answers gets a hollow dot. An
// empty band draws nothing. The caller has checked that every series has the
// same bands.
func svgReliability(title string, ss []series) template.HTML {
	nb := len(ss[0].Bands)
	ymin := 0.5
	for _, s := range ss {
		for _, b := range s.Bands {
			if bandUsed(b) {
				ymin = math.Min(ymin, math.Floor(math.Min(num(b.HitRate), num(b.MeanConf))*10)/10)
			}
		}
	}
	height := relPlotB + 27 + 15*float64(len(ss)) + 8
	yOf := func(v float64) float64 { return relPlotB - (v-ymin)/(1-ymin)*(relPlotB-relTop) }
	sw := (relW - relL - relR) / float64(nb)

	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg class="chart" viewBox="0 0 %d %s" role="img" aria-label="%s" xmlns="http://www.w3.org/2000/svg">`, relW, ff(height), esc(title))
	fmt.Fprintf(&sb, `<title>%s</title>`, esc(title))

	step := tickStep(1 - ymin)
	for v := 1.0; v >= ymin-1e-9; v -= step {
		y := yOf(v)
		fmt.Fprintf(&sb, `<line class="grid" x1="%s" x2="%d" y1="%s" y2="%s"/>`, ff(relL), relW-int(relR), ff(y), ff(y))
		fmt.Fprintf(&sb, `<text class="axis" x="%s" y="%s" text-anchor="end">%.0f%%</text>`, ff(relL-8), ff(y+3.5), v*100)
	}

	for i := 0; i < nb; i++ {
		cx := relL + sw*(float64(i)+0.5)
		b0 := ss[0].Bands[i]
		fmt.Fprintf(&sb, `<text class="axis" x="%s" y="%s" text-anchor="middle">%s</text>`, ff(cx), ff(relPlotB+14), esc(edge(b0.Lo)))
		fmt.Fprintf(&sb, `<text class="axis" x="%s" y="%s" text-anchor="middle">–%s</text>`, ff(cx), ff(relPlotB+27), esc(edge(b0.Hi)))
		for j, s := range ss {
			b := s.Bands[i]
			ny := relPlotB + 27 + 15*float64(j+1)
			if !bandUsed(b) {
				fmt.Fprintf(&sb, `<text class="axis" x="%s" y="%s" text-anchor="middle">–</text>`, ff(cx), ff(ny))
				continue
			}
			fmt.Fprintf(&sb, `<text class="axis" x="%s" y="%s" text-anchor="middle">%d</text>`, ff(cx), ff(ny), b.N)
			x := cx + (float64(j)-float64(len(ss)-1)/2)*18
			yh, ym := yOf(num(b.HitRate)), yOf(num(b.MeanConf))
			cls := slotClass(s.Slot)
			fmt.Fprintf(&sb, `<line class="gap line-%s" x1="%s" x2="%s" y1="%s" y2="%s"/>`, cls, ff(x), ff(x), ff(yh), ff(ym))
			fmt.Fprintf(&sb, `<line class="tick" x1="%s" x2="%s" y1="%s" y2="%s"/>`, ff(x-7), ff(x+7), ff(ym), ff(ym))
			dot := "dot fill-" + cls
			if b.N < minBandN {
				dot = "dot hollow line-" + cls
			}
			fmt.Fprintf(&sb, `<circle class="%s" cx="%s" cy="%s" r="5"/>`, dot, ff(x), ff(yh))
		}
	}
	for j, s := range ss {
		ny := relPlotB + 27 + 15*float64(j+1)
		fmt.Fprintf(&sb, `<circle class="fill-%s" cx="10" cy="%s" r="4"/><text class="axis" x="20" y="%s">n</text>`, slotClass(s.Slot), ff(ny-3.5), ff(ny))
	}
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}

// svgDanger draws the danger zone: how often the model was right when it said
// it was at least `threshold` sure, against the line it claimed. A dot left of
// the line is overconfidence.
func svgDanger(d analysis.Danger, slot int) template.HTML {
	if d.N == 0 || missing(d.HitRate) {
		return ""
	}
	v, th := num(d.HitRate), num(d.Threshold)
	lo, hi := num(d.CI95[0]), num(d.CI95[1])
	low := math.Min(v, th)
	if !math.IsNaN(lo) {
		low = math.Min(low, lo)
	}
	axisLo := math.Min(math.Floor(low*10)/10, 0.9)
	const w, l, r, axisY = 420.0, 16.0, 16.0, 40.0
	xOf := func(p float64) float64 { return l + (p-axisLo)/(1-axisLo)*(w-l-r) }
	cls := slotClass(slot)

	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg class="danger" viewBox="0 0 %s 64" role="img" aria-label="%s" xmlns="http://www.w3.org/2000/svg">`,
		ff(w), esc(fmt.Sprintf("Right %s of the time when it claimed at least %s", pct(d.HitRate), pct(d.Threshold))))
	fmt.Fprintf(&sb, `<line class="grid" x1="%s" x2="%s" y1="%s" y2="%s"/>`, ff(l), ff(w-r), ff(axisY), ff(axisY))
	step := tickStep(1 - axisLo)
	for p := 1.0; p >= axisLo-1e-9; p -= step {
		fmt.Fprintf(&sb, `<text class="axis" x="%s" y="%s" text-anchor="middle">%.0f%%</text>`, ff(xOf(p)), ff(axisY+16), p*100)
	}
	fmt.Fprintf(&sb, `<line class="claim" x1="%s" x2="%s" y1="6" y2="%s"/>`, ff(xOf(th)), ff(xOf(th)), ff(axisY))
	labelX, anchor := xOf(th)+6, "start"
	if xOf(th) > w/2 {
		labelX, anchor = xOf(th)-6, "end"
	}
	fmt.Fprintf(&sb, `<text class="axis" x="%s" y="14" text-anchor="%s">claimed %s or more</text>`, ff(labelX), anchor, pct(d.Threshold))
	if !math.IsNaN(lo) && !math.IsNaN(hi) && hi > lo {
		fmt.Fprintf(&sb, `<line class="range line-%s" x1="%s" x2="%s" y1="26" y2="26"/>`, cls, ff(xOf(lo)), ff(xOf(hi)))
	}
	fmt.Fprintf(&sb, `<circle class="dot fill-%s" cx="%s" cy="26" r="5"/>`, cls, ff(xOf(v)))
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}
