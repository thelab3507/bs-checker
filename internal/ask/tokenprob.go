package ask

import (
	"context"
	"fmt"
	"net/http"

	"github.com/thelab3507/bs-checker/internal/qset"
)

type tokenProb struct {
	client *http.Client
	base   string
	topK   int
}

func (t *tokenProb) Ask(ctx context.Context, q qset.Question) (Answer, error) {
	if err := requireBinary(q); err != nil {
		return Answer{}, err
	}
	pos, neg := q.Options[0], q.Options[1]
	prompt := fmt.Sprintf("Question: %s\nAnswer with one word, %s or %s.", q.Question, pos, neg)
	if q.Passage != "" {
		prompt = q.Passage + "\n\n" + prompt
	}
	body := map[string]any{
		"messages":             []map[string]string{{"role": "user", "content": prompt}},
		"max_tokens":           1,
		"temperature":          0,
		"logprobs":             true,
		"top_logprobs":         t.topK,
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
	}
	var resp struct {
		Choices []struct {
			Logprobs struct {
				Content []struct {
					TopLogprobs []TokenLogprob `json:"top_logprobs"`
				} `json:"content"`
			} `json:"logprobs"`
		} `json:"choices"`
	}
	if err := postJSON(ctx, t.client, t.base+"/v1/chat/completions", body, &resp); err != nil {
		return Answer{}, err
	}
	if len(resp.Choices) == 0 || len(resp.Choices[0].Logprobs.Content) == 0 {
		return Answer{}, fmt.Errorf("response has no logprobs; is the server returning token probabilities?")
	}
	top := resp.Choices[0].Logprobs.Content[0].TopLogprobs
	pPos, mass, ok := yesNoConfidence(top, pos, neg)
	if !ok {
		return Answer{}, fmt.Errorf("no %q or %q token in the top-%d logprobs", pos, neg, t.topK)
	}
	return Answer{Probs: map[string]float64{pos: pPos, neg: 1 - pPos}, AnswerMass: mass}, nil
}
