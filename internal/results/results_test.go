package results

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func sample(host, qid string) Result {
	return Result{Model: "m", Quant: "Q4", Host: host, Set: "s", QID: qid, Method: "tokenprob",
		Chosen: "Yes", Probs: map[string]float64{"Yes": 0.9, "No": 0.1}, PPos: 0.9, PChosen: 0.9,
		AnswerMass: 1, Correct: true, LatencyMS: 12, Timestamp: "2026-11-12T10:00:00Z"}
}

func TestAppendThenLoadDone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.jsonl")
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err := w.Append(sample("mac-metal", id)); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()

	done, err := LoadDone(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 2 || !done[Key("m", "Q4", "mac-metal", "s", "a")] || !done[Key("m", "Q4", "mac-metal", "s", "b")] {
		t.Fatalf("got %v", done)
	}
}

func TestAppendsToAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.jsonl")
	for _, id := range []string{"a", "b"} {
		w, _ := OpenWriter(path)
		w.Append(sample("h", id))
		w.Close()
	}
	data, _ := os.ReadFile(path)
	if n := strings.Count(string(data), "\n"); n != 2 {
		t.Fatalf("lines = %d, want 2", n)
	}
}

func TestKeyDistinguishesHosts(t *testing.T) {
	if sample("mac-metal", "a").Key() == sample("win-cuda", "a").Key() {
		t.Fatal("same key on different hosts")
	}
}

func TestLoadDone(t *testing.T) {
	t.Run("a missing file is an empty set", func(t *testing.T) {
		done, err := LoadDone(filepath.Join(t.TempDir(), "none.jsonl"))
		if err != nil || len(done) != 0 {
			t.Fatalf("got %v, %v", done, err)
		}
	})
	t.Run("a bad line names the file and line", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "r.jsonl")
		os.WriteFile(path, []byte(`{"model":"m"}`+"\n\nnot json\n"), 0o644)
		_, err := LoadDone(path)
		if err == nil || !strings.Contains(err.Error(), "line 3") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestLoadAll(t *testing.T) {
	write := func(t *testing.T, lines ...string) string {
		p := filepath.Join(t.TempDir(), "r.jsonl")
		os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
		return p
	}
	row := func(qid string, pChosen float64, probs string) string {
		return `{"model":"m","quant":"Q4","host":"h","set":"s","qid":"` + qid + `","p_chosen":` + strconv.FormatFloat(pChosen, 'f', -1, 64) + `,"probs":` + probs + `}`
	}
	good := `{"Yes":0.9,"No":0.1}`

	t.Run("reads every row", func(t *testing.T) {
		rows, err := LoadAll(write(t, row("a", 0.9, good), "", row("b", 0.9, good)))
		if err != nil || len(rows) != 2 {
			t.Fatalf("got %d rows, %v", len(rows), err)
		}
	})
	for name, c := range map[string]struct {
		lines []string
		want  string
	}{
		"duplicate answer":       {[]string{row("a", 0.9, good), row("a", 0.9, good)}, "line 2: m|Q4|h|s|a answered twice (first on line 1)"},
		"p_chosen below 0.5":     {[]string{row("a", 0.4, good)}, "line 1: p_chosen 0.4"},
		"p_chosen above 1":       {[]string{row("a", 1.2, good)}, "outside [0.5, 1]"},
		"probs not summing to 1": {[]string{row("a", 0.9, `{"Yes":0.9,"No":0.3}`)}, "summing to 1"},
		"one prob only":          {[]string{row("a", 0.9, `{"Yes":1.0}`)}, "two values"},
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			_, err := LoadAll(write(t, c.lines...))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want it to contain %q", err, c.want)
			}
		})
	}
	t.Run("a missing file is an error", func(t *testing.T) {
		if _, err := LoadAll(filepath.Join(t.TempDir(), "none.jsonl")); err == nil {
			t.Fatal("want error")
		}
	})
}
