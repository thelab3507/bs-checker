// Package results reads and appends the results file: one JSON line per
// answer, synced as it is written so a crash loses nothing and a rerun can
// resume.
package results

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"strings"
)

type Result struct {
	Model      string             `json:"model"`
	Quant      string             `json:"quant"`
	Host       string             `json:"host"`
	Set        string             `json:"set"`
	QID        string             `json:"qid"`
	Method     string             `json:"method"`
	Chosen     string             `json:"chosen"`
	Probs      map[string]float64 `json:"probs"`
	PPos       float64            `json:"p_pos"`
	PChosen    float64            `json:"p_chosen"`
	AnswerMass float64            `json:"answer_mass"`
	Correct    bool               `json:"correct"`
	LatencyMS  int64              `json:"latency_ms"`
	Timestamp  string             `json:"ts"`
}

// Key identifies one answer. The host is part of it so a run on another
// machine is a new run, never silently skipped as done.
func Key(model, quant, host, set, qid string) string {
	return strings.Join([]string{model, quant, host, set, qid}, "|")
}

func (r Result) Key() string { return Key(r.Model, r.Quant, r.Host, r.Set, r.QID) }

// scan calls fn for every non-blank line, parsed. Errors name the file and line.
func scan(path string, fn func(line int, r Result) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r Result
		if err := json.Unmarshal(line, &r); err != nil {
			return fmt.Errorf("%s line %d: %w", path, n, err)
		}
		if err := fn(n, r); err != nil {
			return fmt.Errorf("%s line %d: %w", path, n, err)
		}
	}
	return sc.Err()
}

// LoadDone returns the keys already in the file. A missing file is an empty set.
func LoadDone(path string) (map[string]bool, error) {
	done := map[string]bool{}
	err := scan(path, func(_ int, r Result) error {
		done[r.Key()] = true
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return done, nil
	}
	if err != nil {
		return nil, err
	}
	return done, nil
}

// LoadAll reads every row for analysis and rejects a file the maths cannot
// trust: a repeated answer, a confidence outside [0.5, 1], or probabilities
// that do not sum to 1. A missing file is an error.
func LoadAll(path string) ([]Result, error) {
	var rows []Result
	seen := map[string]int{}
	err := scan(path, func(line int, r Result) error {
		if first, dup := seen[r.Key()]; dup {
			return fmt.Errorf("%s answered twice (first on line %d)", r.Key(), first)
		}
		seen[r.Key()] = line
		if r.PChosen < 0.5-1e-9 || r.PChosen > 1+1e-9 {
			return fmt.Errorf("p_chosen %v is outside [0.5, 1] for %s", r.PChosen, r.Key())
		}
		var sum float64
		for _, p := range r.Probs {
			sum += p
		}
		if len(r.Probs) != 2 || math.Abs(sum-1) > 1e-3 {
			return fmt.Errorf("probs %v for %s must be two values summing to 1", r.Probs, r.Key())
		}
		rows = append(rows, r)
		return nil
	})
	return rows, err
}

type Writer struct{ f *os.File }

func OpenWriter(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &Writer{f: f}, nil
}

func (w *Writer) Append(r Result) error {
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if _, err := w.f.Write(append(line, '\n')); err != nil {
		return err
	}
	return w.f.Sync()
}

func (w *Writer) Close() error { return w.f.Close() }
