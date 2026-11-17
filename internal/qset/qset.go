// Package qset reads, writes and validates question-set JSONL files (the
// format is in the README).
package qset

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"slices"
)

type Question struct {
	ID           string   `json:"id"`
	Set          string   `json:"set"`
	Passage      string   `json:"passage,omitempty"`
	Question     string   `json:"question"`
	Options      []string `json:"options"`
	Answer       string   `json:"answer"`
	VariantGroup string   `json:"variant_group,omitempty"`
	Source       string   `json:"source,omitempty"`
}

func Load(path string) ([]Question, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var qs []Question
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var q Question
		if err := json.Unmarshal(line, &q); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, n, err)
		}
		qs = append(qs, q)
	}
	return qs, sc.Err()
}

func Write(path string, qs []Question) error {
	var buf bytes.Buffer
	for _, q := range qs {
		line, err := json.Marshal(q)
		if err != nil {
			return err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// Validate returns every problem found, so a hand-built set can be fixed in one pass.
func Validate(qs []Question) []error {
	var errs []error
	seen := map[string]bool{}
	for i, q := range qs {
		bad := func(format string, a ...any) {
			errs = append(errs, fmt.Errorf("question %d (%q): %s", i+1, q.ID, fmt.Sprintf(format, a...)))
		}
		if q.ID == "" {
			bad("empty id")
		} else if seen[q.ID] {
			bad("duplicate id")
		}
		seen[q.ID] = true
		if q.Question == "" {
			bad("empty question")
		}
		if q.Set != qs[0].Set {
			bad("set %q differs from the first question's set %q", q.Set, qs[0].Set)
		}
		if len(q.Options) != 2 || q.Options[0] == q.Options[1] {
			bad("options must be exactly two distinct values, got %v", q.Options)
		}
		if !slices.Contains(q.Options, q.Answer) {
			bad("answer %q is not one of the options", q.Answer)
		}
		if q.Source == "" {
			bad("missing source")
		}
	}
	return errs
}
