package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thelab3507/bs-checker/internal/ask"
	"github.com/thelab3507/bs-checker/internal/modelcfg"
	"github.com/thelab3507/bs-checker/internal/qset"
	"github.com/thelab3507/bs-checker/internal/results"
	"github.com/thelab3507/bs-checker/internal/server"
)

const (
	progressEvery = 50
	lowMass       = 0.9
	startTimeout  = 10 * time.Minute
)

type runConfig struct {
	models    modelcfg.Config
	questions []string
	out, host string
	only      []string
	limit     int
	launch    string
	base      string
	logDir    string
	startWait time.Duration
}

type tally struct {
	asked, skipped, correct, lowMass int
	latency                          time.Duration
}

func execute(ctx context.Context, rc runConfig, w io.Writer) error {
	sets, err := loadSets(rc.questions, rc.limit)
	if err != nil {
		return err
	}
	models, err := selectModels(rc.models.Models, rc.only)
	if err != nil {
		return err
	}
	if rc.launch == "none" && len(models) != 1 {
		return fmt.Errorf("-launch none needs -only to pick the one model the running server has loaded, got %d models", len(models))
	}
	done, err := results.LoadDone(rc.out)
	if err != nil {
		return err
	}
	out, err := results.OpenWriter(rc.out)
	if err != nil {
		return err
	}
	defer out.Close()

	for _, m := range models {
		var pending int
		for _, qs := range sets {
			for _, q := range qs {
				if !done[results.Key(m.Label, m.Quant, rc.host, q.Set, q.ID)] {
					pending++
				}
			}
		}
		if pending == 0 {
			fmt.Fprintf(w, "%s %s: nothing to do, all answers already in %s\n", m.Label, m.Quant, rc.out)
			continue
		}
		if err := runModel(ctx, rc, m, sets, done, out, w); err != nil {
			return err
		}
	}
	return nil
}

func runModel(ctx context.Context, rc runConfig, m modelcfg.Model, sets [][]qset.Question, done map[string]bool, out *results.Writer, w io.Writer) error {
	base, logPath := rc.base, ""
	var srv *server.Server
	if rc.launch == "auto" {
		logPath = filepath.Join(rc.logDir, fmt.Sprintf("bsmeter-%s-%s.log", m.Label, m.Quant))
		fmt.Fprintf(w, "%s %s: starting llama-server on port %d (log: %s)\n", m.Label, m.Quant, m.Port, logPath)
		var err error
		srv, err = server.Start(ctx, rc.models.LlamaServer, rc.models.Args(m), m.Port, logPath, rc.startWait)
		if err != nil {
			return fmt.Errorf("%s %s: %w", m.Label, m.Quant, err)
		}
		defer srv.Stop()
		base = srv.URL()
	}
	asker, err := ask.New(m.Method, ask.Options{BaseURL: base})
	if err != nil {
		return fmt.Errorf("%s %s: %w", m.Label, m.Quant, err)
	}

	var t tally
	for _, qs := range sets {
		start := time.Now()
		var todo []qset.Question
		for _, q := range qs {
			if done[results.Key(m.Label, m.Quant, rc.host, q.Set, q.ID)] {
				t.skipped++
			} else {
				todo = append(todo, q)
			}
		}
		for i, q := range todo {
			began := time.Now()
			ans, err := asker.Ask(ctx, q)
			if err != nil {
				if srv != nil && srv.Exited() {
					err = fmt.Errorf("%w (llama-server has exited; log: %s)", err, logPath)
				}
				return fmt.Errorf("model %s %s, question %s: %w", m.Label, m.Quant, q.ID, err)
			}
			r := buildResult(m, rc.host, q, ans, time.Since(began))
			if err := out.Append(r); err != nil {
				return err
			}
			t.add(r)
			if n := i + 1; n%progressEvery == 0 && n < len(todo) {
				eta := time.Since(start) / time.Duration(n) * time.Duration(len(todo)-n)
				fmt.Fprintf(w, "  %s %s %s: %d/%d, about %s left\n", m.Label, m.Quant, q.Set, n, len(todo), eta.Round(time.Second))
			}
		}
	}
	t.print(w, m)
	return nil
}

func (t *tally) add(r results.Result) {
	t.asked++
	t.latency += time.Duration(r.LatencyMS) * time.Millisecond
	if r.Correct {
		t.correct++
	}
	if r.AnswerMass < lowMass {
		t.lowMass++
	}
}

func (t tally) print(w io.Writer, m modelcfg.Model) {
	if t.asked == 0 {
		fmt.Fprintf(w, "%s %s: asked 0, skipped %d already done\n", m.Label, m.Quant, t.skipped)
		return
	}
	fmt.Fprintf(w, "%s %s: asked %d, skipped %d already done, accuracy %.1f%%, mean %d ms",
		m.Label, m.Quant, t.asked, t.skipped, 100*float64(t.correct)/float64(t.asked), (t.latency / time.Duration(t.asked)).Milliseconds())
	if t.lowMass > 0 {
		fmt.Fprintf(w, "\n  WARNING: %d answers put under %.0f%% of their probability on the answer tokens; the model may not be answering Yes/No",
			t.lowMass, 100*lowMass)
	}
	fmt.Fprintln(w)
}

func buildResult(m modelcfg.Model, host string, q qset.Question, a ask.Answer, took time.Duration) results.Result {
	chosen := q.Options[0]
	if a.Probs[q.Options[1]] > a.Probs[q.Options[0]] {
		chosen = q.Options[1]
	}
	return results.Result{
		Model: m.Label, Quant: m.Quant, Host: host, Set: q.Set, QID: q.ID, Method: m.Method,
		Chosen: chosen, Probs: a.Probs, PPos: a.Probs[q.Options[0]], PChosen: a.Probs[chosen],
		AnswerMass: a.AnswerMass, Correct: chosen == q.Answer, LatencyMS: took.Milliseconds(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// loadSets reads and validates each file, keeping the first limit questions of
// each when limit > 0.
func loadSets(paths []string, limit int) ([][]qset.Question, error) {
	var sets [][]qset.Question
	var problems []error
	for _, p := range paths {
		qs, err := qset.Load(p)
		if err != nil {
			return nil, err
		}
		if errs := qset.Validate(qs); len(errs) > 0 {
			problems = append(problems, fmt.Errorf("%s: %w", p, errors.Join(errs...)))
			continue
		}
		if limit > 0 && len(qs) > limit {
			qs = qs[:limit]
		}
		sets = append(sets, qs)
	}
	return sets, errors.Join(problems...)
}

func selectModels(all []modelcfg.Model, only []string) ([]modelcfg.Model, error) {
	if len(only) == 0 {
		return all, nil
	}
	var picked []modelcfg.Model
	for _, want := range only {
		var found bool
		for _, m := range all {
			if m.Label+":"+m.Quant == want {
				picked = append(picked, m)
				found = true
			}
		}
		if !found {
			var have []string
			for _, m := range all {
				have = append(have, m.Label+":"+m.Quant)
			}
			return nil, fmt.Errorf("-only %q matches no model; have %s", want, strings.Join(have, ", "))
		}
	}
	return picked, nil
}

func ensureDir(path string) error {
	if dir := filepath.Dir(path); dir != "." {
		return os.MkdirAll(dir, 0o755)
	}
	return nil
}
