package ask

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/thelab3507/bs-checker/internal/qset"
)

func TestMain(m *testing.M) {
	retryDelay = 0
	m.Run()
}

var yn = qset.Question{ID: "q1", Set: "s", Passage: "A passage.", Question: "Is it so?", Options: []string{"Yes", "No"}, Answer: "Yes", Source: "t"}

func serve(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s.URL
}

func newAsker(t *testing.T, method, base string) Asker {
	t.Helper()
	a, err := New(method, Options{BaseURL: base})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestNewRejectsUnknownMethod(t *testing.T) {
	if _, err := New("magic", Options{}); err == nil {
		t.Fatal("want error")
	}
}

func TestSystemOne(t *testing.T) {
	reply := func(probs string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"answers":{"q":{"probabilities":`+probs+`}}}`)
		}
	}

	t.Run("sends passage as state and question as instructions", func(t *testing.T) {
		var got map[string]any
		base := serve(t, func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&got)
			reply(`{"Yes":0.8,"No":0.2}`)(w, r)
		})
		ans, err := newAsker(t, MethodSystemOne, base).Ask(context.Background(), yn)
		if err != nil {
			t.Fatal(err)
		}
		q := got["questions"].(map[string]any)["q"].(map[string]any)
		if got["state"] != "A passage." || q["instructions"] != "Is it so?" || q["type"] != "choice" {
			t.Fatalf("bad request body: %v", got)
		}
		if ans.Probs["Yes"] != 0.8 || ans.AnswerMass != 1 {
			t.Fatalf("got %+v", ans)
		}
	})

	t.Run("rejects probabilities missing an option", func(t *testing.T) {
		_, err := newAsker(t, MethodSystemOne, serve(t, reply(`{"Yes":1.0}`))).Ask(context.Background(), yn)
		if err == nil || !strings.Contains(err.Error(), `"No"`) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("rejects probabilities that do not sum to 1", func(t *testing.T) {
		_, err := newAsker(t, MethodSystemOne, serve(t, reply(`{"Yes":0.5,"No":0.2}`))).Ask(context.Background(), yn)
		if err == nil || !strings.Contains(err.Error(), "sum") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("names the decision model when nothing comes back", func(t *testing.T) {
		_, err := newAsker(t, MethodSystemOne, serve(t, reply(`{}`))).Ask(context.Background(), yn)
		if err == nil || !strings.Contains(err.Error(), "decision model") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestTokenProb(t *testing.T) {
	chat := func(top string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"choices":[{"logprobs":{"content":[{"top_logprobs":`+top+`}]}}]}`)
		}
	}

	t.Run("builds the frozen prompt with thinking off", func(t *testing.T) {
		var got struct {
			Messages   []map[string]string `json:"messages"`
			MaxTokens  int                 `json:"max_tokens"`
			Temp       float64             `json:"temperature"`
			TopLogprob int                 `json:"top_logprobs"`
			Kwargs     map[string]any      `json:"chat_template_kwargs"`
		}
		base := serve(t, func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&got)
			chat(`[{"token":"Yes","logprob":-0.1},{"token":"No","logprob":-2.4}]`)(w, r)
		})
		ans, err := newAsker(t, MethodTokenProb, base).Ask(context.Background(), yn)
		if err != nil {
			t.Fatal(err)
		}
		want := "A passage.\n\nQuestion: Is it so?\nAnswer with one word, Yes or No."
		if got.Messages[0]["content"] != want || got.MaxTokens != 1 || got.Temp != 0 || got.TopLogprob != 20 || got.Kwargs["enable_thinking"] != false {
			t.Fatalf("bad request: %+v", got)
		}
		if math.Abs(ans.Probs["Yes"]+ans.Probs["No"]-1) > 1e-9 || ans.Probs["Yes"] < 0.9 {
			t.Fatalf("got %+v", ans)
		}
	})

	t.Run("errors when no Yes or No token appears", func(t *testing.T) {
		_, err := newAsker(t, MethodTokenProb, serve(t, chat(`[{"token":"<|channel>","logprob":-0.01}]`))).Ask(context.Background(), yn)
		if err == nil || !strings.Contains(err.Error(), "top-20") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("errors when the server returns no logprobs", func(t *testing.T) {
		base := serve(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"choices":[{}]}`) })
		if _, err := newAsker(t, MethodTokenProb, base).Ask(context.Background(), yn); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("omits the passage line when there is none", func(t *testing.T) {
		var got struct {
			Messages []map[string]string `json:"messages"`
		}
		base := serve(t, func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&got)
			chat(`[{"token":"No","logprob":-0.1}]`)(w, r)
		})
		q := yn
		q.Passage = ""
		if _, err := newAsker(t, MethodTokenProb, base).Ask(context.Background(), q); err != nil {
			t.Fatal(err)
		}
		if got.Messages[0]["content"] != "Question: Is it so?\nAnswer with one word, Yes or No." {
			t.Fatalf("got %q", got.Messages[0]["content"])
		}
	})
}

func TestNonBinaryQuestionRejected(t *testing.T) {
	q := yn
	q.Options = []string{"A", "B", "C"}
	for _, m := range []string{MethodSystemOne, MethodTokenProb} {
		if _, err := newAsker(t, m, "http://unused").Ask(context.Background(), q); err == nil {
			t.Fatalf("%s: want error", m)
		}
	}
}

func TestPostJSONRetries(t *testing.T) {
	t.Run("retries a 503 then succeeds", func(t *testing.T) {
		var calls atomic.Int32
		base := serve(t, func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) < 3 {
				http.Error(w, "busy", http.StatusServiceUnavailable)
				return
			}
			io.WriteString(w, `{"answers":{"q":{"probabilities":{"Yes":0.6,"No":0.4}}}}`)
		})
		if _, err := newAsker(t, MethodSystemOne, base).Ask(context.Background(), yn); err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 3 {
			t.Fatalf("calls = %d, want 3", calls.Load())
		}
	})

	t.Run("does not retry a 400", func(t *testing.T) {
		var calls atomic.Int32
		base := serve(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			http.Error(w, "bad body", http.StatusBadRequest)
		})
		_, err := newAsker(t, MethodSystemOne, base).Ask(context.Background(), yn)
		if err == nil || !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "bad body") {
			t.Fatalf("got %v", err)
		}
		if calls.Load() != 1 {
			t.Fatalf("calls = %d, want 1", calls.Load())
		}
	})

	t.Run("gives up after three 500s", func(t *testing.T) {
		var calls atomic.Int32
		base := serve(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			http.Error(w, "too large", http.StatusInternalServerError)
		})
		if _, err := newAsker(t, MethodSystemOne, base).Ask(context.Background(), yn); err == nil {
			t.Fatal("want error")
		}
		if calls.Load() != 3 {
			t.Fatalf("calls = %d, want 3", calls.Load())
		}
	})

	t.Run("stops at once when the context is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := newAsker(t, MethodSystemOne, "http://127.0.0.1:1").Ask(ctx, yn); err == nil {
			t.Fatal("want error")
		}
	})
}
