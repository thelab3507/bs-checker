// Command mkdata builds and checks question sets (the format is in the README).
//
//	mkdata boolq [-n 600] [-seed 1] [-out data/boolq.jsonl]
//	mkdata sms [-n 500] [-seed 1] [-out data/sms-spam.jsonl]
//	mkdata validate FILE...
//	mkdata sample-ids [-pct 10] [-seed 1] FILE
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/thelab3507/bs-checker/internal/qset"
)

func main() {
	if len(os.Args) < 2 {
		fatal("usage: mkdata boolq|sms|validate|sample-ids ...")
	}
	args := os.Args[2:]
	var err error
	switch os.Args[1] {
	case "boolq":
		err = runBoolQ(args)
	case "sms":
		err = runSMS(args)
	case "validate":
		err = runValidate(args)
	case "sample-ids":
		err = runSampleIDs(args)
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fatal(err.Error())
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "mkdata:", msg)
	os.Exit(1)
}

func runBoolQ(args []string) error {
	fs := flag.NewFlagSet("boolq", flag.ExitOnError)
	n := fs.Int("n", 600, "questions to sample")
	seed := fs.Uint64("seed", 1, "sampling seed")
	out := fs.String("out", "data/boolq.jsonl", "output file")
	fs.Parse(args)

	rows, err := fetchBoolQ(&http.Client{Timeout: 60 * time.Second}, boolqRowsURL, boolqRows)
	if err != nil {
		return err
	}
	picks, err := sample(make([]bool, len(rows)), *n, false, *seed)
	if err != nil {
		return err
	}
	qs := make([]qset.Question, len(picks))
	for i, idx := range picks {
		qs[i] = boolqToQuestion(rows[idx], idx)
	}
	return finish(*out, qs)
}

func runSMS(args []string) error {
	fs := flag.NewFlagSet("sms", flag.ExitOnError)
	n := fs.Int("n", 500, "messages to sample, half spam and half ham")
	seed := fs.Uint64("seed", 1, "sampling seed")
	out := fs.String("out", "data/sms-spam.jsonl", "output file")
	fs.Parse(args)

	rows, err := fetchSMS(&http.Client{Timeout: 60 * time.Second}, smsZipURL, smsMessages)
	if err != nil {
		return err
	}
	spam := make([]bool, len(rows))
	for i, r := range rows {
		spam[i] = r.Spam
	}
	picks, err := sample(spam, *n, true, *seed)
	if err != nil {
		return err
	}
	qs := make([]qset.Question, len(picks))
	for i, idx := range picks {
		qs[i] = smsToQuestion(rows[idx], idx)
	}
	return finish(*out, qs)
}

func finish(path string, qs []qset.Question) error {
	if errs := qset.Validate(qs); len(errs) > 0 {
		return errs[0]
	}
	if err := qset.Write(path, qs); err != nil {
		return err
	}
	fmt.Printf("wrote %d questions to %s\n", len(qs), path)
	return nil
}

func runValidate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("validate needs at least one file")
	}
	failed := false
	for _, path := range args {
		qs, err := qset.Load(path)
		if err != nil {
			return err
		}
		errs := qset.Validate(qs)
		yes := 0
		for _, q := range qs {
			if q.Answer == q.Options[0] {
				yes++
			}
		}
		fmt.Printf("%s: %d questions, %d%% first-option answers, %d problems\n", path, len(qs), 100*yes/max(len(qs), 1), len(errs))
		for _, e := range errs {
			fmt.Println("  ", e)
			failed = true
		}
	}
	if failed {
		return fmt.Errorf("validation failed")
	}
	return nil
}

// runSampleIDs prints a reproducible pct% of question IDs for the manual spot-check.
func runSampleIDs(args []string) error {
	fs := flag.NewFlagSet("sample-ids", flag.ExitOnError)
	pct := fs.Int("pct", 10, "percentage of questions to print")
	seed := fs.Uint64("seed", 1, "sampling seed")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("sample-ids needs exactly one file")
	}
	qs, err := qset.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	picks, err := sample(make([]bool, len(qs)), (len(qs)*(*pct)+99)/100, false, *seed)
	if err != nil {
		return err
	}
	for _, i := range picks {
		fmt.Println(qs[i].ID)
	}
	return nil
}
