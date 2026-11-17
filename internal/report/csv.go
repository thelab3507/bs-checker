package report

import (
	"encoding/csv"
	"io"
	"math"
	"strconv"

	"github.com/thelab3507/bs-checker/internal/analysis"
)

func cell(n analysis.Num) string {
	f := num(n)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return ""
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func measureCells(m analysis.Measure) []string {
	return []string{cell(m.Value), cell(m.CI95[0]), cell(m.CI95[1])}
}

// WriteRunsCSV writes one row per run: the numbers behind each card, plus the verdict.
func WriteRunsCSV(w io.Writer, doc analysis.Doc) error {
	cw := csv.NewWriter(w)
	head := []string{"model", "quant", "host", "method", "set", "n", "n_clusters", "verdict"}
	for _, m := range []string{"accuracy", "ece", "ece_fine", "brier", "log_loss", "gap"} {
		head = append(head, m, m+"_lo", m+"_hi")
	}
	head = append(head, "danger_threshold", "danger_n", "danger_wrong", "danger_hit_rate", "danger_lo", "danger_hi",
		"correction_t", "correction_ece_before", "correction_ece_after", "correction_nll_before", "correction_nll_after", "low_mass_n")
	if err := cw.Write(head); err != nil {
		return err
	}
	for _, r := range doc.Runs {
		row := []string{r.Model, r.Quant, r.Host, r.Method, r.Set, strconv.Itoa(r.N), strconv.Itoa(r.NClusters), VerdictFor(r).Key}
		for _, m := range []analysis.Measure{r.Accuracy, r.ECE, r.ECEFine, r.Brier, r.LogLoss, r.Gap} {
			row = append(row, measureCells(m)...)
		}
		d := r.DangerZone
		row = append(row, cell(d.Threshold), strconv.Itoa(d.N), strconv.Itoa(d.Wrong), cell(d.HitRate), cell(d.CI95[0]), cell(d.CI95[1]))
		if c := r.Correction; c != nil {
			row = append(row, cell(c.Params["T"]), cell(c.ECEBefore), cell(c.ECEAfter), cell(c.NLLBefore), cell(c.NLLAfter))
		} else {
			row = append(row, "", "", "", "", "")
		}
		row = append(row, strconv.Itoa(r.LowMassN))
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
