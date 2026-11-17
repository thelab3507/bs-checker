package ask

import (
	"math"
	"strings"
	"unicode"
)

type TokenLogprob struct {
	Token   string  `json:"token"`
	Logprob float64 `json:"logprob"`
}

func normaliseToken(tok string) string {
	trimmed := strings.TrimFunc(tok, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return strings.ToLower(trimmed)
}

// yesNoConfidence returns the probability of pos normalised over pos and neg,
// plus the raw probability mass the two answers hold in the top-k list.
// A side missing from the list is treated as negligible; ok is false only
// when neither side appears, so the caller can report it instead of faking 50%.
func yesNoConfidence(top []TokenLogprob, pos, neg string) (pPos, mass float64, ok bool) {
	posKey, negKey := normaliseToken(pos), normaliseToken(neg)
	var massPos, massNeg float64
	for _, t := range top {
		switch normaliseToken(t.Token) {
		case posKey:
			massPos += math.Exp(t.Logprob)
		case negKey:
			massNeg += math.Exp(t.Logprob)
		}
	}
	mass = massPos + massNeg
	if mass == 0 {
		return 0, 0, false
	}
	return massPos / mass, mass, true
}
