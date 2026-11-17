package calib

import "math"

// Temperature search bounds. A fit that lands on a bound is reported, not
// trusted: it means the data cannot pin T down (for example every fit answer
// was right, which pushes T towards 0).
const (
	MinT = 0.05
	MaxT = 20.0
)

func logit(p float64) float64 { p = clamp(p); return math.Log(p / (1 - p)) }

func sigmoid(z float64) float64 { return 1 / (1 + math.Exp(-z)) }

// ApplyTemperature divides the confidence's log-odds by t. T above 1 softens,
// below 1 sharpens. Confidence stays at or above 0.5, so no answer changes and
// accuracy is untouched.
func ApplyTemperature(obs []Obs, t float64) []Obs {
	out := make([]Obs, len(obs))
	for i, o := range obs {
		o.Conf = sigmoid(logit(o.Conf) / t)
		out[i] = o
	}
	return out
}

// FitTemperature returns the T that minimises log loss on obs. Log loss is
// convex in w = 1/T, so a golden-section search on w finds the optimum.
func FitTemperature(obs []Obs) (t float64, atBound bool) {
	loss := func(w float64) float64 { return LogLoss(ApplyTemperature(obs, 1/w)) }
	lo, hi := 1/MaxT, 1/MinT
	const phi = 0.6180339887498949
	a, b := lo, hi
	c, d := b-phi*(b-a), a+phi*(b-a)
	fc, fd := loss(c), loss(d)
	for i := 0; i < 80; i++ {
		if fc < fd {
			b, d, fd = d, c, fc
			c = b - phi*(b-a)
			fc = loss(c)
		} else {
			a, c, fc = c, d, fd
			d = a + phi*(b-a)
			fd = loss(d)
		}
	}
	w := (a + b) / 2
	atBound = w-lo < 1e-3*lo || hi-w < 1e-3*hi
	return 1 / w, atBound
}
