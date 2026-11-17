// Command bsmeter asks a model list a set of yes/no questions and records how
// confident each answer was.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/thelab3507/bs-checker/internal/modelcfg"
)

const usage = `usage: bsmeter <command> [flags]

  run      ask every model in the model list every question and append the
           answers to a results file; a rerun skips answers already recorded
  analyze  turn a results file into accuracy, calibration, error bars and a
           saved correction (analysis.json)
  report   turn analysis.json into one self-contained HTML page, and
           optionally CSV summaries`

type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(v string) error { *l = append(*l, v); return nil }

func main() {
	if len(os.Args) < 2 {
		fatal(usage)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = run(os.Args[2:])
	case "analyze":
		err = analyze(os.Args[2:], os.Stderr)
	case "report":
		err = reportCmd(os.Args[2:], os.Stderr)
	default:
		fatal(usage)
	}
	if err != nil {
		fatal(err.Error())
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "bsmeter:", msg)
	os.Exit(1)
}

func run(args []string) error {
	var rc runConfig
	var modelsPath string
	var qs, only list
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	fs.StringVar(&modelsPath, "models", "models.json", "model list")
	fs.Var(&qs, "questions", "question-set JSONL (repeatable; default: the three sets in data/)")
	fs.StringVar(&rc.out, "out", "results.jsonl", "results JSONL (appended, resumable)")
	fs.StringVar(&rc.host, "host", "", "machine and backend, e.g. mac-metal (required)")
	fs.Var(&only, "only", "run only label:quant (repeatable)")
	fs.IntVar(&rc.limit, "limit", 0, "ask only the first N questions of each set (0 = all)")
	fs.StringVar(&rc.launch, "launch", "auto", "auto: start llama-server per model; none: use the server at -base")
	fs.StringVar(&rc.base, "base", "http://127.0.0.1:9931", "server URL for -launch none")
	fs.Parse(args)

	if rc.host == "" {
		return fmt.Errorf("-host is required, e.g. -host mac-metal")
	}
	if rc.launch != "auto" && rc.launch != "none" {
		return fmt.Errorf("-launch must be auto or none")
	}
	if len(qs) == 0 {
		qs = list{"data/boolq.jsonl", "data/sms-spam.jsonl", "data/fresh-news.jsonl"}
	}
	cfg, err := modelcfg.Load(modelsPath)
	if err != nil {
		return err
	}
	rc.models, rc.questions, rc.only = cfg, qs, only
	rc.logDir, rc.startWait = os.TempDir(), startTimeout
	if err := ensureDir(rc.out); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err = execute(ctx, rc, os.Stderr)
	if ctx.Err() != nil {
		return fmt.Errorf("interrupted; every answer so far is saved, rerun the same command to resume")
	}
	return err
}
