// Package ask gets a binary answer and its confidence from a running
// llama-server, either from the native decision endpoint or from Yes/No token
// probabilities.
package ask

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/thelab3507/bs-checker/internal/qset"
)

const (
	MethodSystemOne = "systemone"
	MethodTokenProb = "tokenprob"
)

// Answer holds the probability of each option, summing to 1, and the raw
// probability mass the model put on the two answers. For systemone the mass is
// always 1; for tokenprob a low mass means the model is not answering Yes/No.
type Answer struct {
	Probs      map[string]float64
	AnswerMass float64
}

type Asker interface {
	Ask(ctx context.Context, q qset.Question) (Answer, error)
}

type Options struct {
	BaseURL string
	TopK    int
}

func New(method string, o Options) (Asker, error) {
	client := &http.Client{Timeout: 120 * time.Second}
	switch method {
	case MethodSystemOne:
		return &systemOne{client: client, base: o.BaseURL}, nil
	case MethodTokenProb:
		topK := o.TopK
		if topK == 0 {
			topK = 20
		}
		return &tokenProb{client: client, base: o.BaseURL, topK: topK}, nil
	}
	return nil, fmt.Errorf("unknown method %q, want %s or %s", method, MethodSystemOne, MethodTokenProb)
}

func requireBinary(q qset.Question) error {
	if len(q.Options) != 2 {
		return fmt.Errorf("only binary questions are supported, got %d options", len(q.Options))
	}
	return nil
}
