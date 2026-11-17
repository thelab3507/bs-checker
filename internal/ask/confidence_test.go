package ask

import (
	"math"
	"testing"
)

func TestYesNoConfidence(t *testing.T) {
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

	t.Run("merges Yes, ' yes' and 'YES' into one side", func(t *testing.T) {
		top := []TokenLogprob{
			{"Yes", math.Log(0.5)}, {" yes", math.Log(0.2)}, {"YES", math.Log(0.1)},
			{"No", math.Log(0.2)},
		}
		p, mass, ok := yesNoConfidence(top, "Yes", "No")
		if !ok || !near(p, 0.8) || !near(mass, 1.0) {
			t.Fatalf("got p=%v mass=%v ok=%v, want 0.8 1.0 true", p, mass, ok)
		}
	})

	t.Run("normalises mass over the two sides so pPos + pNeg = 1", func(t *testing.T) {
		top := []TokenLogprob{{"Yes", math.Log(0.3)}, {"No", math.Log(0.1)}, {"Maybe", math.Log(0.6)}}
		p, mass, ok := yesNoConfidence(top, "Yes", "No")
		if !ok || !near(p, 0.75) || !near(mass, 0.4) {
			t.Fatalf("got p=%v mass=%v ok=%v, want 0.75 0.4 true", p, mass, ok)
		}
	})

	t.Run("returns ok=false when neither Yes nor No is in top-k", func(t *testing.T) {
		top := []TokenLogprob{{"<|channel>", -0.005}, {"Maybe", -6}}
		if _, _, ok := yesNoConfidence(top, "Yes", "No"); ok {
			t.Fatal("want ok=false")
		}
	})

	t.Run("ignores unrelated tokens in the top-k list", func(t *testing.T) {
		top := []TokenLogprob{{"Year", math.Log(0.4)}, {"Yes.", math.Log(0.3)}, {"Node", math.Log(0.2)}, {"No", math.Log(0.1)}}
		p, _, ok := yesNoConfidence(top, "Yes", "No")
		if !ok || !near(p, 0.75) {
			t.Fatalf("got p=%v ok=%v, want 0.75 true", p, ok)
		}
	})
}
