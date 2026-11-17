package qset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func good(id string) Question {
	return Question{ID: id, Set: "s", Question: "Q?", Options: []string{"Yes", "No"}, Answer: "Yes", Source: "x"}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		qs   []Question
		want []string
	}{
		{"clean set passes", []Question{good("a"), good("b")}, nil},
		{"duplicate id", []Question{good("a"), good("a")}, []string{"duplicate id"}},
		{"answer not in options", []Question{func() Question { q := good("a"); q.Answer = "Maybe"; return q }()}, []string{"not one of the options"}},
		{"three options", []Question{func() Question { q := good("a"); q.Options = []string{"A", "B", "C"}; return q }()}, []string{"exactly two"}},
		{"identical options", []Question{func() Question { q := good("a"); q.Options = []string{"Yes", "Yes"}; return q }()}, []string{"exactly two"}},
		{"mixed sets", []Question{good("a"), func() Question { q := good("b"); q.Set = "other"; return q }()}, []string{"differs from the first"}},
		{"missing source", []Question{func() Question { q := good("a"); q.Source = ""; return q }()}, []string{"missing source"}},
		{"all problems reported", []Question{{ID: "a", Set: "s", Options: []string{"Yes"}, Answer: "No"}}, []string{"empty question", "exactly two", "not one of the options", "missing source"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var all []string
			for _, e := range Validate(tt.qs) {
				all = append(all, e.Error())
			}
			joined := strings.Join(all, "\n")
			if len(tt.want) == 0 && len(all) != 0 {
				t.Fatalf("expected no errors, got %v", all)
			}
			for _, w := range tt.want {
				if !strings.Contains(joined, w) {
					t.Errorf("missing error containing %q in:\n%s", w, joined)
				}
			}
		})
	}
}

func TestWriteLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.jsonl")
	in := []Question{good("a"), func() Question { q := good("b"); q.Passage = "p\twith tab"; return q }()}
	if err := Write(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[1].Passage != "p\twith tab" || out[0].ID != "a" {
		t.Fatalf("round trip mismatch: %+v", out)
	}
}

func TestLoadNamesBadLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.jsonl")
	os.WriteFile(path, []byte(`{"id":"a"}`+"\n\n"+`{not json}`+"\n"), 0o644)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("expected an error naming line 3, got %v", err)
	}
}
