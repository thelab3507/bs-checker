package calib

import (
	"math"
	"strconv"
	"testing"
)

func TestFitRecoversTheTrueTemperature(t *testing.T) {
	t.Run("calibrated model gives T near 1", func(t *testing.T) {
		T, atBound := FitTemperature(fake(20000, 1, newRng(1)))
		if atBound || !near(T, 1, 0.05) {
			t.Fatalf("T = %v atBound=%v, want within 5%% of 1", T, atBound)
		}
	})
	t.Run("overconfident model gives T near 2", func(t *testing.T) {
		T, atBound := FitTemperature(fake(20000, 2, newRng(2)))
		if atBound || !near(T, 2, 0.2) {
			t.Fatalf("T = %v atBound=%v, want within 10%% of 2", T, atBound)
		}
	})
	t.Run("underconfident model gives T under 1", func(t *testing.T) {
		T, _ := FitTemperature(fake(20000, 0.5, newRng(3)))
		if T >= 1 || !near(T, 0.5, 0.05) {
			t.Fatalf("T = %v, want near 0.5", T)
		}
	})
}

func TestCorrectionHelpsOnHeldOutHalf(t *testing.T) {
	obs := fake(4000, 2, newRng(4))
	fit, eval := SplitClusters(obs, newRng(5))
	T, _ := FitTemperature(fit)
	after := ApplyTemperature(eval, T)

	if before, got := ECE(eval, tenBands), ECE(after, tenBands); got >= before {
		t.Fatalf("held-out ECE %v -> %v, want a fall", before, got)
	}
	if before, got := LogLoss(eval), LogLoss(after); got >= before {
		t.Fatalf("held-out log loss %v -> %v, want a fall", before, got)
	}
	if Accuracy(eval) != Accuracy(after) {
		t.Fatal("temperature scaling must not change any answer")
	}
	for i := range after {
		if after[i].Conf < 0.5 || after[i].Correct != eval[i].Correct || after[i].Cluster != eval[i].Cluster {
			t.Fatalf("row %d changed beyond confidence: %+v vs %+v", i, after[i], eval[i])
		}
	}
}

func TestSeparableDataPinsTheFitAtTheLowerBound(t *testing.T) {
	obs := make([]Obs, 40)
	for i := range obs {
		obs[i] = Obs{Conf: 0.9, Correct: true, Cluster: strconv.Itoa(i)}
	}
	T, atBound := FitTemperature(obs)
	if !atBound || !near(T, MinT, 1e-3) {
		t.Fatalf("T = %v atBound=%v, want the lower bound %v flagged", T, atBound, MinT)
	}
}

func TestApplyTemperatureMovesConfidenceTheRightWay(t *testing.T) {
	obs := []Obs{{0.9, true, "a"}}
	if soft := ApplyTemperature(obs, 2)[0].Conf; soft >= 0.9 || soft <= 0.5 {
		t.Fatalf("T=2 should soften 0.9, got %v", soft)
	}
	if sharp := ApplyTemperature(obs, 0.5)[0].Conf; sharp <= 0.9 {
		t.Fatalf("T=0.5 should sharpen 0.9, got %v", sharp)
	}
	if same := ApplyTemperature(obs, 1)[0].Conf; !near(same, 0.9, 1e-12) {
		t.Fatalf("T=1 should leave 0.9 alone, got %v", same)
	}
	if v := ApplyTemperature([]Obs{{1.0, false, "x"}}, 1)[0].Conf; math.IsNaN(v) || v > 1 {
		t.Fatalf("confidence 1.0 must stay finite, got %v", v)
	}
}
