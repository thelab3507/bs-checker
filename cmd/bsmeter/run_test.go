package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thelab3507/bs-checker/internal/modelcfg"
	"github.com/thelab3507/bs-checker/internal/qset"
	"github.com/thelab3507/bs-checker/internal/results"
)

// fakeMux answers like llama-server: Yes with 0.9 on /v1/systemone, and Yes at
// logprob -0.1 against No at -2.4 on chat completions. If lowMass is set the
// answer tokens hold under 1% of the probability. After dieAfter requests (when
// positive) onDie runs.
func fakeMux(lowMass bool, dieAfter int, onDie func()) *http.ServeMux {
	var n atomic.Int32
	count := func() {
		if dieAfter > 0 && int(n.Add(1)) > dieAfter {
			onDie()
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"status":"ok"}`) })
	mux.HandleFunc("/v1/systemone", func(w http.ResponseWriter, r *http.Request) {
		count()
		io.WriteString(w, `{"answers":{"q":{"probabilities":{"Yes":0.9,"No":0.1}}}}`)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		count()
		yes, no := -0.1, -2.4
		if lowMass {
			yes, no = -6.0, -7.0
		}
		fmt.Fprintf(w, `{"choices":[{"logprobs":{"content":[{"top_logprobs":[{"token":"Yes","logprob":%g},{"token":"No","logprob":%g}]}]}}]}`, yes, no)
	})
	return mux
}

// TestMain doubles as a fake llama-server for the -launch auto tests.
func TestMain(m *testing.M) {
	if os.Getenv("BSM_FAKE_SERVER") != "" {
		var port string
		for i, a := range os.Args {
			if a == "--port" {
				port = os.Args[i+1]
			}
		}
		die, _ := strconv.Atoi(os.Getenv("BSM_FAKE_DIE_AFTER"))
		http.ListenAndServe("127.0.0.1:"+port, fakeMux(false, die, func() { os.Exit(7) }))
		return
	}
	os.Exit(m.Run())
}

func writeSet(t *testing.T, dir, set string, n int) string {
	t.Helper()
	var qs []qset.Question
	for i := 1; i <= n; i++ {
		ans := "Yes"
		if i%3 == 0 {
			ans = "No"
		}
		qs = append(qs, qset.Question{ID: fmt.Sprintf("%s-%02d", set, i), Set: set, Passage: "p", Question: "Is it?",
			Options: []string{"Yes", "No"}, Answer: ans, Source: "test"})
	}
	p := filepath.Join(dir, set+".jsonl")
	if err := qset.Write(p, qs); err != nil {
		t.Fatal(err)
	}
	return p
}

func entry(label, quant, method string, port int) modelcfg.Model {
	return modelcfg.Model{Label: label, Quant: quant, Method: method, HF: "x/y:" + quant, Port: port}
}

func baseConfig(t *testing.T, base string, models ...modelcfg.Model) runConfig {
	t.Helper()
	dir := t.TempDir()
	return runConfig{
		models:    modelcfg.Config{LlamaServer: "llama-server", DefaultArgs: modelcfg.DefaultArgs, Models: models},
		questions: []string{writeSet(t, dir, "s1", 10), writeSet(t, dir, "s2", 4)},
		out:       filepath.Join(dir, "results.jsonl"),
		host:      "test-host",
		launch:    "none",
		base:      base,
		logDir:    dir,
		startWait: 10 * time.Second,
	}
}

func readRows(t *testing.T, path string) []results.Result {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []results.Result
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var r results.Result
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("bad line %q: %v", line, err)
		}
		rows = append(rows, r)
	}
	return rows
}

func run1(t *testing.T, rc runConfig) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	err := execute(context.Background(), rc, &buf)
	return buf.String(), err
}

func fakeServerURL(t *testing.T, lowMass bool) string {
	t.Helper()
	s := httptest.NewServer(fakeMux(lowMass, 0, nil))
	t.Cleanup(s.Close)
	return s.URL
}

func TestRunWritesOneFrozenFormatRowPerQuestion(t *testing.T) {
	for _, method := range []string{"tokenprob", "systemone"} {
		t.Run(method, func(t *testing.T) {
			rc := baseConfig(t, fakeServerURL(t, false), entry("m", "Q4", method, 9000))
			rc.only = []string{"m:Q4"}
			if _, err := run1(t, rc); err != nil {
				t.Fatal(err)
			}
			rows := readRows(t, rc.out)
			if len(rows) != 14 {
				t.Fatalf("rows = %d, want 14", len(rows))
			}
			r := rows[0]
			if r.Model != "m" || r.Quant != "Q4" || r.Host != "test-host" || r.Set != "s1" || r.QID != "s1-01" ||
				r.Method != method || r.Chosen != "Yes" || !r.Correct || r.PPos != r.PChosen ||
				r.PChosen < 0.89 || r.PChosen > 0.91 || r.Timestamp == "" {
				t.Fatalf("bad row: %+v", r)
			}
			if r.Probs["Yes"]+r.Probs["No"] < 0.999999 {
				t.Fatalf("probs do not sum to 1: %v", r.Probs)
			}
			if rows[2].Correct {
				t.Fatalf("question 3's answer is No, so the Yes pick must be wrong: %+v", rows[2])
			}
		})
	}
}

func TestRerunAsksNothingAndNeedsNoServer(t *testing.T) {
	rc := baseConfig(t, fakeServerURL(t, false), entry("m", "Q4", "tokenprob", 9000))
	if _, err := run1(t, rc); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(rc.out)

	rc.launch, rc.models.LlamaServer = "auto", "definitely-not-llama-server"
	out, err := run1(t, rc)
	if err != nil {
		t.Fatalf("a finished run must not try to start a server: %v", err)
	}
	after, _ := os.ReadFile(rc.out)
	if !bytes.Equal(before, after) || !strings.Contains(out, "nothing to do") {
		t.Fatalf("rerun changed the file or printed %q", out)
	}
}

func TestTruncatedFileResumesWithoutDuplicates(t *testing.T) {
	rc := baseConfig(t, fakeServerURL(t, false), entry("m", "Q4", "tokenprob", 9000))
	if _, err := run1(t, rc); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(rc.out)
	lines := bytes.SplitAfter(bytes.TrimSpace(data), []byte("\n"))
	os.WriteFile(rc.out, bytes.Join(lines[:7], nil), 0o644)

	out, err := run1(t, rc)
	if err != nil {
		t.Fatal(err)
	}
	rows := readRows(t, rc.out)
	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.Key()] {
			t.Fatalf("duplicate %s", r.Key())
		}
		seen[r.Key()] = true
	}
	if len(rows) != 14 || !strings.Contains(out, "asked 7, skipped 7") {
		t.Fatalf("rows = %d, output %q", len(rows), out)
	}
}

func TestAnotherHostAsksEverythingAgain(t *testing.T) {
	rc := baseConfig(t, fakeServerURL(t, false), entry("m", "Q4", "tokenprob", 9000))
	if _, err := run1(t, rc); err != nil {
		t.Fatal(err)
	}
	rc.host = "other-host"
	if _, err := run1(t, rc); err != nil {
		t.Fatal(err)
	}
	if n := len(readRows(t, rc.out)); n != 28 {
		t.Fatalf("rows = %d, want 28", n)
	}
}

func TestLimitTakesTheFirstNOfEachSet(t *testing.T) {
	rc := baseConfig(t, fakeServerURL(t, false), entry("m", "Q4", "tokenprob", 9000))
	rc.limit = 3
	if _, err := run1(t, rc); err != nil {
		t.Fatal(err)
	}
	rows := readRows(t, rc.out)
	if len(rows) != 6 || rows[2].QID != "s1-03" || rows[3].QID != "s2-01" {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
}

func TestLowAnswerMassIsRecordedAndWarned(t *testing.T) {
	rc := baseConfig(t, fakeServerURL(t, true), entry("m", "Q4", "tokenprob", 9000))
	out, err := run1(t, rc)
	if err != nil {
		t.Fatal(err)
	}
	if rows := readRows(t, rc.out); len(rows) != 14 || rows[0].AnswerMass > 0.01 {
		t.Fatalf("low-mass rows must still be recorded, got %d rows, mass %v", len(rows), rows[0].AnswerMass)
	}
	if !strings.Contains(out, "WARNING: 14 answers") {
		t.Fatalf("no warning in %q", out)
	}
}

func TestSummaryReportsAccuracy(t *testing.T) {
	rc := baseConfig(t, fakeServerURL(t, false), entry("m", "Q4", "tokenprob", 9000))
	out, err := run1(t, rc)
	if err != nil {
		t.Fatal(err)
	}
	// "Yes" is right unless i%3 == 0: s1 (10 questions) has 3 No, s2 (4) has 1, so 10 of 14.
	if !strings.Contains(out, "asked 14, skipped 0") || !strings.Contains(out, "accuracy 71.4%") {
		t.Fatalf("summary = %q", out)
	}
}

func TestBadSelectionsAreClearErrors(t *testing.T) {
	rc := baseConfig(t, fakeServerURL(t, false), entry("m", "Q4", "tokenprob", 9000), entry("m", "Q8", "tokenprob", 9000))

	if _, err := run1(t, rc); err == nil || !strings.Contains(err.Error(), "-launch none needs -only") {
		t.Fatalf("launch none with two models: %v", err)
	}
	rc.only = []string{"nope:Q4"}
	if _, err := run1(t, rc); err == nil || !strings.Contains(err.Error(), "m:Q4, m:Q8") {
		t.Fatalf("unknown -only should list what exists: %v", err)
	}
	rc.only = nil
	rc.launch = "auto"
	rc.questions = []string{filepath.Join(t.TempDir(), "missing.jsonl")}
	if _, err := run1(t, rc); err == nil {
		t.Fatal("missing question file: want error")
	}
}

func TestInvalidQuestionSetIsRejectedBeforeAnyCall(t *testing.T) {
	rc := baseConfig(t, "http://127.0.0.1:1", entry("m", "Q4", "tokenprob", 9000))
	bad := filepath.Join(t.TempDir(), "bad.jsonl")
	os.WriteFile(bad, []byte(`{"id":"a","set":"s","question":"q","options":["Yes","No"],"answer":"Maybe","source":"t"}`+"\n"), 0o644)
	rc.questions = []string{bad}
	if _, err := run1(t, rc); err == nil || !strings.Contains(err.Error(), "bad.jsonl") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(rc.out); err == nil {
		t.Fatal("results file must not be created for an invalid set")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func portFree(port int) bool {
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

func TestLaunchAutoRunsEachModelThenFreesThePort(t *testing.T) {
	t.Setenv("BSM_FAKE_SERVER", "1")
	port := freePort(t)
	rc := baseConfig(t, "", entry("a", "Q4", "tokenprob", port), entry("a", "Q8", "systemone", port))
	rc.launch, rc.models.LlamaServer = "auto", os.Args[0]
	if _, err := run1(t, rc); err != nil {
		t.Fatal(err)
	}
	rows := readRows(t, rc.out)
	if len(rows) != 28 || rows[0].Quant != "Q4" || rows[14].Quant != "Q8" {
		t.Fatalf("rows = %d", len(rows))
	}
	if !portFree(port) {
		t.Fatal("server left running")
	}
}

func TestServerCrashMidRunKeepsEarlierRowsAndNamesTheLog(t *testing.T) {
	t.Setenv("BSM_FAKE_SERVER", "1")
	t.Setenv("BSM_FAKE_DIE_AFTER", "3")
	port := freePort(t)
	rc := baseConfig(t, "", entry("a", "Q4", "tokenprob", port))
	rc.launch, rc.models.LlamaServer = "auto", os.Args[0]
	_, err := run1(t, rc)
	if err == nil || !strings.Contains(err.Error(), "a Q4, question s1-04") || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("got %v", err)
	}
	if n := len(readRows(t, rc.out)); n != 3 {
		t.Fatalf("rows = %d, want the 3 answered before the crash", n)
	}
	if !portFree(port) {
		t.Fatal("server left running")
	}
}

func TestRunRequiresAHost(t *testing.T) {
	if err := run([]string{"-launch", "none"}); err == nil || !strings.Contains(err.Error(), "-host") {
		t.Fatalf("got %v", err)
	}
}
