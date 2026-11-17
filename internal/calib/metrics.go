// Package calib holds the calibration maths: reliability bands, ECE, scores
// that need no binning, temperature scaling and cluster-aware bootstrapping.
// It does no I/O and knows nothing about models or files.
package calib

import "math"

// Obs is one answer: the confidence in the chosen option (always in [0.5, 1]),
// whether it was right, and the cluster it belongs to. Answers in one cluster
// (for example questions about one news story) are resampled together.
type Obs struct {
	Conf    float64
	Correct bool
	Cluster string
}

type Band struct {
	Lo, Hi   float64
	N        int
	MeanConf float64
	HitRate  float64
}

// FineEdges spreads a saturated model over the top end, where 10 equal bands
// put nearly everything in one band. The cut points are fixed, not tuned to data.
var FineEdges = []float64{0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 0.99, 0.999, 0.9999, 1.0}

// EqualEdges returns k+1 edges splitting [lo, hi] into k equal bands, rounded
// so a boundary such as 0.7 is exactly the float literal 0.7.
func EqualEdges(lo, hi float64, k int) []float64 {
	e := make([]float64, k+1)
	for i := range e {
		e[i] = math.Round((lo+(hi-lo)*float64(i)/float64(k))*1e9) / 1e9
	}
	return e
}

// Bands groups answers into [edges[i], edges[i+1]); the last band includes its
// upper edge. Empty bands have N == 0 and zero means.
func Bands(obs []Obs, edges []float64) []Band {
	bands := make([]Band, len(edges)-1)
	conf := make([]float64, len(bands))
	hits := make([]int, len(bands))
	for i := range bands {
		bands[i].Lo, bands[i].Hi = edges[i], edges[i+1]
	}
	last := len(bands) - 1
	for _, o := range obs {
		for i := range bands {
			if o.Conf >= bands[i].Lo && (o.Conf < bands[i].Hi || (i == last && o.Conf <= bands[i].Hi)) {
				bands[i].N++
				conf[i] += o.Conf
				if o.Correct {
					hits[i]++
				}
				break
			}
		}
	}
	for i := range bands {
		if bands[i].N > 0 {
			bands[i].MeanConf = conf[i] / float64(bands[i].N)
			bands[i].HitRate = float64(hits[i]) / float64(bands[i].N)
		}
	}
	return bands
}

// ECE is the expected calibration error: the average gap between claimed
// confidence and hit rate, weighted by how many answers fall in each band.
func ECE(obs []Obs, edges []float64) float64 {
	if len(obs) == 0 {
		return math.NaN()
	}
	var e float64
	for _, b := range Bands(obs, edges) {
		e += float64(b.N) / float64(len(obs)) * math.Abs(b.MeanConf-b.HitRate)
	}
	return e
}

func Accuracy(obs []Obs) float64 {
	if len(obs) == 0 {
		return math.NaN()
	}
	var c int
	for _, o := range obs {
		if o.Correct {
			c++
		}
	}
	return float64(c) / float64(len(obs))
}

// Gap is mean confidence minus accuracy: positive when the model claims more
// than it delivers (overconfident), negative when it claims less. Unlike ECE it
// is signed, so over- and underconfident bands can cancel; read it with ECE.
func Gap(obs []Obs) float64 {
	if len(obs) == 0 {
		return math.NaN()
	}
	var sum float64
	for _, o := range obs {
		sum += o.Conf
	}
	return sum/float64(len(obs)) - Accuracy(obs)
}

// Brier is the mean squared gap between confidence and outcome. Unlike ECE it
// needs no bands, so it stays meaningful for a saturated model.
func Brier(obs []Obs) float64 {
	if len(obs) == 0 {
		return math.NaN()
	}
	var s float64
	for _, o := range obs {
		d := clamp(o.Conf)
		if o.Correct {
			d = 1 - d
		}
		s += d * d
	}
	return s / float64(len(obs))
}

// LogLoss is the mean negative log probability given to what happened. A wrong
// answer at confidence 1.0 costs a large finite number, not infinity.
func LogLoss(obs []Obs) float64 {
	if len(obs) == 0 {
		return math.NaN()
	}
	var s float64
	for _, o := range obs {
		p := clamp(o.Conf)
		if !o.Correct {
			p = 1 - p
		}
		s -= math.Log(p)
	}
	return s / float64(len(obs))
}

// DangerZone returns how many answers were at or above threshold and the share
// of them that were right. ok is false when there are none.
func DangerZone(obs []Obs, threshold float64) (n int, hitRate float64, ok bool) {
	var hits int
	for _, o := range obs {
		if o.Conf >= threshold {
			n++
			if o.Correct {
				hits++
			}
		}
	}
	if n == 0 {
		return 0, 0, false
	}
	return n, float64(hits) / float64(n), true
}

const eps = 1e-12

func clamp(p float64) float64 { return math.Min(math.Max(p, eps), 1-eps) }
