package calib

import (
	"math"
	"math/rand/v2"
	"strconv"
	"testing"
)

func TestBootstrapIntervalCoversTheTruth(t *testing.T) {
	const trials, truth = 200, 0.7
	rng := newRng(10)
	var covered int
	for trial := 0; trial < trials; trial++ {
		obs := make([]Obs, 400)
		for i := range obs {
			obs[i] = Obs{Conf: 0.7, Correct: rng.Float64() < truth, Cluster: strconv.Itoa(i)}
		}
		lo, hi := Bootstrap(obs, 300, rng, Accuracy)
		if lo <= truth && truth <= hi {
			covered++
		}
	}
	if rate := float64(covered) / trials; rate < 0.88 || rate > 1.0 {
		t.Fatalf("coverage = %.3f, want 0.88 to 1.0 for a 95%% interval", rate)
	}
}

// clustered builds 20 clusters of 10 answers whose hit rate is 0.1 or 0.9 for
// the whole cluster, so answers in a cluster are far from independent. With
// byCluster false every answer is its own cluster instead.
func clustered(byCluster bool, rng *rand.Rand) []Obs {
	var obs []Obs
	for c := 0; c < 20; c++ {
		p := 0.1
		if c%2 == 0 {
			p = 0.9
		}
		for i := 0; i < 10; i++ {
			key := "c" + strconv.Itoa(c)
			if !byCluster {
				key += "-" + strconv.Itoa(i)
			}
			obs = append(obs, Obs{Conf: 0.8, Correct: rng.Float64() < p, Cluster: key})
		}
	}
	return obs
}

func TestClusterBootstrapIsWiderThanResamplingRows(t *testing.T) {
	width := func(byCluster bool) float64 {
		lo, hi := Bootstrap(clustered(byCluster, newRng(20)), 1000, newRng(21), Accuracy)
		return hi - lo
	}
	byCluster, byRow := width(true), width(false)
	if byCluster < 1.5*byRow {
		t.Fatalf("cluster interval %.3f vs row interval %.3f, want over 1.5x wider", byCluster, byRow)
	}
}

func TestBootstrapIsDeterministicForASeed(t *testing.T) {
	obs := fake(300, 1, newRng(1))
	l1, h1 := Bootstrap(obs, 200, newRng(7), Accuracy)
	l2, h2 := Bootstrap(obs, 200, newRng(7), Accuracy)
	if l1 != l2 || h1 != h2 {
		t.Fatal("same seed gave different intervals")
	}
}

func TestBootstrapDropsUndefinedResamples(t *testing.T) {
	obs := []Obs{{0.99, true, "a"}, {0.6, true, "b"}, {0.6, false, "c"}}
	lo, hi := Bootstrap(obs, 200, newRng(1), func(o []Obs) float64 {
		_, hit, ok := DangerZone(o, 0.95)
		if !ok {
			return math.NaN()
		}
		return hit
	})
	if math.IsNaN(lo) || math.IsNaN(hi) {
		t.Fatalf("got [%v, %v], want the defined resamples to give an interval", lo, hi)
	}
	nl, nh := Bootstrap(obs[1:], 50, newRng(1), func([]Obs) float64 { return math.NaN() })
	if !math.IsNaN(nl) || !math.IsNaN(nh) {
		t.Fatal("all-undefined resamples must give NaN, not a number")
	}
}

func TestSplitClustersKeepsEachClusterOnOneSide(t *testing.T) {
	var obs []Obs
	for c := 0; c < 10; c++ {
		for i := 0; i < 5; i++ {
			obs = append(obs, Obs{Conf: 0.8, Correct: true, Cluster: "c" + strconv.Itoa(c)})
		}
	}
	fit, eval := SplitClusters(obs, newRng(1))
	if len(fit) != 25 || len(eval) != 25 {
		t.Fatalf("sizes %d and %d, want 25 and 25", len(fit), len(eval))
	}
	side := map[string]bool{}
	for _, o := range fit {
		side[o.Cluster] = true
	}
	for _, o := range eval {
		if side[o.Cluster] {
			t.Fatalf("cluster %s is on both sides", o.Cluster)
		}
	}
	fit2, _ := SplitClusters(obs, newRng(1))
	if len(fit2) != len(fit) || fit2[0].Cluster != fit[0].Cluster {
		t.Fatal("same seed gave a different split")
	}
}
