package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	pageDelay = 0
	os.Exit(m.Run())
}

func TestBoolqToQuestion(t *testing.T) {
	q := boolqToQuestion(boolqRow{Question: "does ethanol take more energy make that produces", Answer: true, Passage: "P."}, 7)
	if q.Question != "Does ethanol take more energy make that produces?" {
		t.Errorf("question = %q", q.Question)
	}
	if q.Answer != "Yes" || q.ID != "boolq-0007" || q.Passage != "P." || q.Set != "boolq" {
		t.Errorf("unexpected question %+v", q)
	}
	if boolqToQuestion(boolqRow{Question: "is it?", Answer: false}, 0).Question != "Is it?" {
		t.Error("an existing question mark must not be doubled")
	}
	if boolqToQuestion(boolqRow{Answer: false}, 0).Answer != "No" {
		t.Error("false must map to No")
	}
}

func TestSmsToQuestion(t *testing.T) {
	spam := smsToQuestion(smsRow{Spam: true, Text: "WIN a prize"}, 3)
	ham := smsToQuestion(smsRow{Spam: false, Text: "see you at 5"}, 4)
	if spam.Answer != "Yes" || ham.Answer != "No" {
		t.Errorf("spam=%s ham=%s", spam.Answer, ham.Answer)
	}
	if spam.Passage != "WIN a prize" || spam.Question != "Is this message spam?" || spam.ID != "sms-0003" {
		t.Errorf("unexpected question %+v", spam)
	}
}

func labels(pos, neg int) []bool {
	l := make([]bool, 0, pos+neg)
	for range pos {
		l = append(l, true)
	}
	for range neg {
		l = append(l, false)
	}
	return l
}

func TestSample(t *testing.T) {
	l := labels(30, 70)
	a, _ := sample(l, 20, false, 1)
	b, _ := sample(l, 20, false, 1)
	c, _ := sample(l, 20, false, 2)
	if !slices.Equal(a, b) {
		t.Error("same seed must give the same sample")
	}
	if slices.Equal(a, c) {
		t.Error("different seeds should give different samples")
	}
	if !slices.IsSorted(a) || len(a) != 20 {
		t.Errorf("sample not sorted or wrong size: %v", a)
	}

	s, err := sample(l, 20, true, 1)
	if err != nil {
		t.Fatal(err)
	}
	pos := 0
	for _, i := range s {
		if l[i] {
			pos++
		}
	}
	if pos != 10 || len(s) != 20 {
		t.Errorf("stratified sample has %d positives of %d", pos, len(s))
	}

	if _, err := sample(labels(3, 100), 20, true, 1); err == nil || !strings.Contains(err.Error(), "positive class") {
		t.Errorf("expected a positive-class shortage error, got %v", err)
	}
}

func boolqStub(total int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var off, length int
		fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &off)
		fmt.Sscanf(r.URL.Query().Get("length"), "%d", &length)
		type row struct {
			Row boolqRow `json:"row"`
		}
		var rows []row
		for i := off; i < min(off+length, total); i++ {
			rows = append(rows, row{boolqRow{Question: fmt.Sprintf("q%d", i), Answer: i%2 == 0, Passage: "p"}})
		}
		json.NewEncoder(w).Encode(map[string]any{"rows": rows, "num_rows_total": total})
	}))
}

func TestFetchBoolQPagesThroughEveryRowOnce(t *testing.T) {
	srv := boolqStub(250)
	defer srv.Close()
	rows, err := fetchBoolQ(srv.Client(), srv.URL+"/rows?x=1", 250)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 250 || rows[0].Question != "q0" || rows[249].Question != "q249" {
		t.Errorf("got %d rows, first %q last %q", len(rows), rows[0].Question, rows[len(rows)-1].Question)
	}
}

func TestFetchBoolQRejectsWrongCount(t *testing.T) {
	srv := boolqStub(250)
	defer srv.Close()
	if _, err := fetchBoolQ(srv.Client(), srv.URL+"/rows?x=1", 300); err == nil || !strings.Contains(err.Error(), "expected 300") {
		t.Errorf("expected a count error, got %v", err)
	}
}

func TestFetchSMSKeepsTabsInsideMessages(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("SMSSpamCollection")
	f.Write([]byte("ham\tsee you\tat five\nspam\tWIN now\n"))
	zw.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(buf.Bytes()) }))
	defer srv.Close()

	rows, err := fetchSMS(srv.Client(), srv.URL, 2)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Text != "see you\tat five" || rows[0].Spam || !rows[1].Spam {
		t.Errorf("unexpected rows %+v", rows)
	}
}
