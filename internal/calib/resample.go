package calib

import (
	"math"
	"math/rand/v2"
	"slices"
	"sort"
)

// GroupByCluster returns the cluster keys in sorted order and each cluster's answers.
func GroupByCluster(obs []Obs) ([]string, map[string][]Obs) {
	groups := map[string][]Obs{}
	for _, o := range obs {
		groups[o.Cluster] = append(groups[o.Cluster], o)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys, groups
}

// SplitClusters shuffles the clusters and puts the first half in fit and the
// rest in eval, so no cluster is on both sides.
func SplitClusters(obs []Obs, rng *rand.Rand) (fit, eval []Obs) {
	keys, groups := GroupByCluster(obs)
	rng.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
	for i, k := range keys {
		if i < len(keys)/2 {
			fit = append(fit, groups[k]...)
		} else {
			eval = append(eval, groups[k]...)
		}
	}
	return fit, eval
}

// Bootstrap resamples whole clusters with replacement b times and returns the
// 95% percentile interval of stat. Resamples where stat is undefined (NaN) are
// dropped; if all are, the result is NaN.
func Bootstrap(obs []Obs, b int, rng *rand.Rand, stat func([]Obs) float64) (lo, hi float64) {
	keys, groups := GroupByCluster(obs)
	vals := make([]float64, 0, b)
	buf := make([]Obs, 0, len(obs))
	for i := 0; i < b; i++ {
		buf = buf[:0]
		for range keys {
			buf = append(buf, groups[keys[rng.IntN(len(keys))]]...)
		}
		vals = append(vals, stat(buf))
	}
	return percentileInterval(vals)
}

func percentileInterval(vals []float64) (lo, hi float64) {
	v := vals[:0:0]
	for _, x := range vals {
		if !math.IsNaN(x) {
			v = append(v, x)
		}
	}
	if len(v) == 0 {
		return math.NaN(), math.NaN()
	}
	sort.Float64s(v)
	n := float64(len(v))
	return v[int(math.Floor(0.025*n))], v[int(math.Ceil(0.975*n))-1]
}
