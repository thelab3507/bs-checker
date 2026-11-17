package modelcfg

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestShippedConfigLoads(t *testing.T) {
	for _, p := range []string{"../../models.json", "../../examples/models.example.json"} {
		c, err := Load(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if len(c.Models) != 3 {
			t.Fatalf("%s: %d models, want 3", p, len(c.Models))
		}
		labels := map[string]bool{}
		for _, m := range c.Models {
			labels[m.Label] = true
			if m.Quant != "Q4_K_M" {
				t.Errorf("%s: %s is %s, want every shipped model at Q4_K_M", p, m.Label, m.Quant)
			}
		}
		for _, want := range []string{"lfm2-d1-2b", "kev-4b", "gemma-4-e2b-it"} {
			if !labels[want] {
				t.Errorf("%s: no %s entry", p, want)
			}
		}
	}
}

func TestDefaults(t *testing.T) {
	c, err := Load(write(t, `{"models":[{"label":"a","quant":"Q4","method":"tokenprob","hf":"x/y:Q4","port":9000}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.LlamaServer != "llama-server" {
		t.Fatalf("llama_server = %q", c.LlamaServer)
	}
	args := c.Args(c.Models[0])
	want := []string{"-hf", "x/y:Q4", "-b", "2048", "-ub", "2048", "-c", "4096", "-np", "1"}
	if !slices.Equal(args, want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

func TestExtraArgsFollowDefaults(t *testing.T) {
	c, err := Load(write(t, `{"default_args":["-b","512"],"models":[{"label":"a","quant":"Q4","method":"tokenprob","hf":"x:Q4","port":9000,"extra_args":["--no-mmproj"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-hf", "x:Q4", "-b", "512", "--no-mmproj"}
	if got := c.Args(c.Models[0]); !slices.Equal(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
}

func TestReportsEveryProblem(t *testing.T) {
	_, err := Load(write(t, `{"models":[
		{"label":"a","quant":"Q4","method":"magic","hf":"x:Q4","port":9000},
		{"label":"a","quant":"Q4","method":"tokenprob","hf":"","port":80}
	]}`))
	if err == nil {
		t.Fatal("want error")
	}
	for _, want := range []string{`method "magic"`, "duplicate", "hf is required", "port 80"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%v", want, err)
		}
	}
}

func TestRejectsEmptyListAndBadJSON(t *testing.T) {
	if _, err := Load(write(t, `{"models":[]}`)); err == nil {
		t.Fatal("empty list: want error")
	}
	if _, err := Load(write(t, `{`)); err == nil {
		t.Fatal("bad json: want error")
	}
}
