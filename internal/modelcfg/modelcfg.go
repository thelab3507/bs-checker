// Package modelcfg loads the model list (see the README): which models to run,
// at which quantisation, and how to read their confidence.
package modelcfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/thelab3507/bs-checker/internal/ask"
)

// DefaultArgs are passed to every llama-server launch. -b/-ub 2048 are
// required: at the default 512, d1-2B rejects the longest BoolQ passages.
var DefaultArgs = []string{"-b", "2048", "-ub", "2048", "-c", "4096", "-np", "1"}

type Model struct {
	Label     string   `json:"label"`
	Quant     string   `json:"quant"`
	Method    string   `json:"method"`
	HF        string   `json:"hf"`
	Port      int      `json:"port"`
	ExtraArgs []string `json:"extra_args,omitempty"`
}

type Config struct {
	LlamaServer string   `json:"llama_server"`
	DefaultArgs []string `json:"default_args"`
	Models      []Model  `json:"models"`
}

// Args returns the full llama-server argument list for m, without --host/--port.
func (c Config) Args(m Model) []string {
	args := append([]string{"-hf", m.HF}, c.DefaultArgs...)
	return append(args, m.ExtraArgs...)
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if c.LlamaServer == "" {
		c.LlamaServer = "llama-server"
	}
	if c.DefaultArgs == nil {
		c.DefaultArgs = DefaultArgs
	}
	if errs := validate(c); len(errs) > 0 {
		return Config{}, fmt.Errorf("%s: %w", path, errors.Join(errs...))
	}
	return c, nil
}

func validate(c Config) []error {
	var errs []error
	if len(c.Models) == 0 {
		errs = append(errs, errors.New("no models listed"))
	}
	seen := map[string]bool{}
	for i, m := range c.Models {
		at := func(msg string) error { return fmt.Errorf("model %d (%s %s): %s", i+1, m.Label, m.Quant, msg) }
		if m.Label == "" || m.Quant == "" {
			errs = append(errs, at("label and quant are required"))
		}
		key := m.Label + "|" + m.Quant
		if seen[key] {
			errs = append(errs, at("duplicate label and quant"))
		}
		seen[key] = true
		if m.Method != ask.MethodSystemOne && m.Method != ask.MethodTokenProb {
			errs = append(errs, at(fmt.Sprintf("method %q, want %s or %s", m.Method, ask.MethodSystemOne, ask.MethodTokenProb)))
		}
		if m.HF == "" {
			errs = append(errs, at("hf is required, e.g. LiquidAI/d1-2B-GGUF:Q4_K_M"))
		}
		if m.Port < 1024 || m.Port > 65535 {
			errs = append(errs, at(fmt.Sprintf("port %d out of range 1024-65535", m.Port)))
		}
	}
	return errs
}
