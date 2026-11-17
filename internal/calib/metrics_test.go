package calib

import (
	"math"
	"math/rand/v2"
	"strconv"
	"testing"
)

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func newRng(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, 99)) }

// fake builds n answers. For each, the truth is right with probability q drawn
// uniform in (0.5, 0.999), and the model reports the log-odds of q multiplied
// by sharp: 1 is calibrated, above 1 overconfident, below 1 underconfident.
// Every answer is its own cluster.
func fake(n int, sharp float64, rng *rand.Rand) []Obs {
	obs := make([]Obs, n)
	for i := range obs {
		q := 0.5 + 0.499*rng.Float64()
		obs[i] = Obs{Conf: sigmoid(sharp * logit(q)), Correct: rng.Float64() < q, Cluster: strconv.Itoa(i)}
	}
	return obs
}

var tenBands = EqualEdges(0.5, 1.0, 10)

func TestEqualEdgesAreExact(t *testing.T) {
	if len(tenBands) != 11 || tenBands[0] != 0.5 || tenBands[4] != 0.7 || tenBands[10] != 1.0 {
		t.Fatalf("edges = %v", tenBands)
	}
}

// Hand-worked: .55 right, .58 wrong, .95 right, .97 right, 1.0 right, .96 wrong.
// Ten bands of width .05. Band [.55,.6): 2 answers, mean .565, hit .5, gap .065.
// Band [.95,1.0] (last, closed): 4 answers, mean .97, hit .75, gap .22.
// ECE = (2*.065 + 4*.22) / 6.
var sixRows = []Obs{
	{0.55, true, "a"}, {0.58, false, "b"}, {0.95, true, "c"}, {0.97, true, "d"}, {1.0, true, "e"}, {0.96, false, "f"},
}

func TestBandsAndECEByHand(t *testing.T) {
	bands := Bands(sixRows, tenBands)
	if len(bands) != 10 {
		t.Fatalf("bands = %d", len(bands))
	}
	if b := bands[1]; b.N != 2 || !near(b.MeanConf, 0.565, 1e-12) || !near(b.HitRate, 0.5, 1e-12) {
		t.Fatalf("band 1 = %+v", b)
	}
	if b := bands[0]; b.N != 0 || b.MeanConf != 0 || b.HitRate != 0 {
		t.Fatalf("empty band = %+v", b)
	}
	if b := bands[9]; b.N != 4 || !near(b.MeanConf, 0.97, 1e-12) || !near(b.HitRate, 0.75, 1e-12) {
		t.Fatalf("closed top band must hold 1.0: %+v", b)
	}
	if got, want := ECE(sixRows, tenBands), (2*0.065+4*0.22)/6; !near(got, want, 1e-12) {
		t.Fatalf("ECE = %v, want %v", got, want)
	}
}

func TestBandBoundaryBelongsToTheUpperBand(t *testing.T) {
	bands := Bands([]Obs{{0.7, true, "a"}}, tenBands)
	if bands[3].N != 0 || bands[4].N != 1 {
		t.Fatalf("0.7 should be in [0.7, 0.75): %+v", bands)
	}
}

func TestBrierAndLogLossByHand(t *testing.T) {
	wantBrier := (0.45*0.45 + 0.58*0.58 + 0.05*0.05 + 0.03*0.03 + 0 + 0.96*0.96) / 6
	if got := Brier(sixRows); !near(got, wantBrier, 1e-9) {
		t.Fatalf("Brier = %v, want %v", got, wantBrier)
	}
	wantLL := -(math.Log(0.55) + math.Log(0.42) + math.Log(0.95) + math.Log(0.97) + math.Log(1-1e-12) + math.Log(0.04)) / 6
	if got := LogLoss(sixRows); !near(got, wantLL, 1e-9) {
		t.Fatalf("LogLoss = %v, want %v", got, wantLL)
	}
	if got := Accuracy(sixRows); !near(got, 4.0/6, 1e-12) {
		t.Fatalf("Accuracy = %v", got)
	}
}

func TestGapIsSignedMeanConfidenceMinusAccuracy(t *testing.T) {
	// mean conf (.55+.58+.95+.97+1+.96)/6 = .835, accuracy 4/6.
	if got, want := Gap(sixRows), (0.55+0.58+0.95+0.97+1.0+0.96)/6-4.0/6; !near(got, want, 1e-12) {
		t.Fatalf("Gap = %v, want %v", got, want)
	}
	if g := Gap([]Obs{{0.6, true, "a"}, {0.6, true, "b"}}); g >= 0 {
		t.Fatalf("underconfident gap = %v, want negative", g)
	}
	if !math.IsNaN(Gap(nil)) {
		t.Fatal("Gap of nothing must be NaN")
	}
	if g := Gap(fake(20000, 1, newRng(1))); math.Abs(g) > 0.02 {
		t.Fatalf("calibrated gap = %v", g)
	}
	if g := Gap(fake(20000, 2, newRng(1))); g < 0.05 {
		t.Fatalf("overconfident gap = %v, want clearly positive", g)
	}
}

func TestWrongAtFullConfidenceIsLargeButFinite(t *testing.T) {
	ll := LogLoss([]Obs{{1.0, false, "a"}})
	if math.IsInf(ll, 0) || math.IsNaN(ll) || ll < 20 {
		t.Fatalf("LogLoss = %v, want large and finite", ll)
	}
}

func TestEmptyInputIsNaNNotZero(t *testing.T) {
	for name, v := range map[string]float64{
		"ECE": ECE(nil, tenBands), "Accuracy": Accuracy(nil), "Brier": Brier(nil), "LogLoss": LogLoss(nil),
	} {
		if !math.IsNaN(v) {
			t.Errorf("%s of nothing = %v, want NaN", name, v)
		}
	}
}

func TestDangerZone(t *testing.T) {
	obs := []Obs{{0.95, true, "a"}, {0.94, false, "b"}, {0.99, false, "c"}, {1.0, true, "d"}}
	n, hit, ok := DangerZone(obs, 0.95)
	if !ok || n != 3 || !near(hit, 2.0/3, 1e-12) {
		t.Fatalf("got n=%d hit=%v ok=%v; threshold must be inclusive", n, hit, ok)
	}
	if _, _, ok := DangerZone(obs[1:2], 0.95); ok {
		t.Fatal("no answers at or above the threshold must report ok=false")
	}
}

func TestCalibratedModelHasNearZeroECE(t *testing.T) {
	obs := fake(20000, 1, newRng(1))
	if e := ECE(obs, tenBands); e > 0.02 {
		t.Fatalf("calibrated ECE = %v, want under 0.02", e)
	}
	if e := ECE(fake(20000, 2, newRng(1)), tenBands); e < 0.05 {
		t.Fatalf("overconfident ECE = %v, want clearly above a calibrated model", e)
	}
}

func TestSaturatedModelSpreadsOverFineBandsOnly(t *testing.T) {
	rng := newRng(3)
	obs := make([]Obs, 500)
	for i := range obs {
		obs[i] = Obs{Conf: 1 - math.Pow(10, -(3+2*rng.Float64())), Correct: rng.Float64() < 0.99, Cluster: strconv.Itoa(i)}
	}
	occupied := func(edges []float64) int {
		var n int
		for _, b := range Bands(obs, edges) {
			if b.N > 0 {
				n++
			}
		}
		return n
	}
	if got := occupied(tenBands); got != 1 {
		t.Fatalf("10 equal bands occupied = %d, want 1", got)
	}
	if got := occupied(FineEdges); got < 2 {
		t.Fatalf("fine bands occupied = %d, want at least 2", got)
	}
}
