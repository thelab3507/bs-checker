package ask

import (
	"context"
	"fmt"
	"math"
	"net/http"

	"github.com/thelab3507/bs-checker/internal/qset"
)

type systemOne struct {
	client *http.Client
	base   string
}

func (s *systemOne) Ask(ctx context.Context, q qset.Question) (Answer, error) {
	if err := requireBinary(q); err != nil {
		return Answer{}, err
	}
	criteria := map[string]any{}
	for _, o := range q.Options {
		criteria[o] = nil
	}
	body := map[string]any{
		"state": q.Passage,
		"questions": map[string]any{"q": map[string]any{
			"type": "choice", "instructions": q.Question, "criteria": criteria,
		}},
	}
	var resp struct {
		Answers map[string]struct {
			Probabilities map[string]float64 `json:"probabilities"`
		} `json:"answers"`
	}
	if err := postJSON(ctx, s.client, s.base+"/v1/systemone", body, &resp); err != nil {
		return Answer{}, err
	}
	p := resp.Answers["q"].Probabilities
	if len(p) == 0 {
		return Answer{}, fmt.Errorf("/v1/systemone returned no probabilities; is this a decision model?")
	}
	var sum float64
	for _, o := range q.Options {
		v, ok := p[o]
		if !ok {
			return Answer{}, fmt.Errorf("/v1/systemone probabilities have no %q entry: %v", o, p)
		}
		sum += v
	}
	if math.Abs(sum-1) > 1e-3 {
		return Answer{}, fmt.Errorf("/v1/systemone probabilities sum to %.4f, want 1: %v", sum, p)
	}
	return Answer{Probs: map[string]float64{q.Options[0]: p[q.Options[0]], q.Options[1]: p[q.Options[1]]}, AnswerMass: 1}, nil
}
