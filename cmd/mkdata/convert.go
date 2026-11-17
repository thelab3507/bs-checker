package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/thelab3507/bs-checker/internal/qset"
)

const (
	boolqRowsURL  = "https://datasets-server.huggingface.co/rows?dataset=google/boolq&config=default&split=validation"
	boolqRows     = 3270
	smsZipURL     = "https://archive.ics.uci.edu/static/public/228/sms+spam+collection.zip"
	smsMessages   = 5574
	boolqPageSize = 100
)

var yesNo = []string{"Yes", "No"}

type boolqRow struct {
	Question string `json:"question"`
	Answer   bool   `json:"answer"`
	Passage  string `json:"passage"`
}

type smsRow struct {
	Spam bool
	Text string
}

func yn(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

// BoolQ questions are lowercase with no question mark ("does ethanol take more energy").
func boolqToQuestion(r boolqRow, idx int) qset.Question {
	text := strings.TrimSpace(r.Question)
	first, size := utf8.DecodeRuneInString(text)
	text = string(unicode.ToUpper(first)) + text[size:]
	if !strings.HasSuffix(text, "?") {
		text += "?"
	}
	return qset.Question{
		ID: fmt.Sprintf("boolq-%04d", idx), Set: "boolq", Passage: r.Passage, Question: text,
		Options: yesNo, Answer: yn(r.Answer), Source: "google/boolq validation, CC BY-SA 3.0",
	}
}

func smsToQuestion(r smsRow, idx int) qset.Question {
	return qset.Question{
		ID: fmt.Sprintf("sms-%04d", idx), Set: "sms-spam", Passage: r.Text, Question: "Is this message spam?",
		Options: yesNo, Answer: yn(r.Spam), Source: "UCI SMS Spam Collection, CC BY 4.0",
	}
}

// sample returns sorted indices into labels. When stratified, n/2 are drawn from each label.
func sample(labels []bool, n int, stratified bool, seed uint64) ([]int, error) {
	rng := rand.New(rand.NewPCG(seed, seed))
	pick := func(pool []int, k int) ([]int, error) {
		if len(pool) < k {
			return nil, fmt.Errorf("need %d items but only %d available", k, len(pool))
		}
		rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
		return pool[:k], nil
	}

	var out []int
	if stratified {
		var pos, neg []int
		for i, l := range labels {
			if l {
				pos = append(pos, i)
			} else {
				neg = append(neg, i)
			}
		}
		p, err := pick(pos, n/2)
		if err != nil {
			return nil, fmt.Errorf("positive class: %w", err)
		}
		q, err := pick(neg, n-n/2)
		if err != nil {
			return nil, fmt.Errorf("negative class: %w", err)
		}
		out = append(p, q...)
	} else {
		all := make([]int, len(labels))
		for i := range all {
			all[i] = i
		}
		var err error
		if out, err = pick(all, n); err != nil {
			return nil, err
		}
	}
	slices.Sort(out)
	return out, nil
}

// pageDelay spaces out requests; the Hugging Face rows API answers 429 to a fast loop.
var pageDelay = 500 * time.Millisecond

func getWithRetry(client *http.Client, url string) ([]byte, error) {
	var lastErr error
	wait := time.Duration(0)
	for attempt := range 6 {
		if attempt > 0 {
			time.Sleep(wait)
		}
		resp, err := client.Get(url)
		if err != nil {
			lastErr, wait = err, time.Duration(attempt+1)*2*time.Second
			continue
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr, wait = err, time.Duration(attempt+1)*2*time.Second
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("%s returned %d", url, resp.StatusCode)
			wait = time.Duration(attempt+1) * 5 * time.Second
			if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
				wait = time.Duration(secs) * time.Second
			}
			continue
		}
		return data, nil
	}
	return nil, lastErr
}

func fetchBoolQ(client *http.Client, baseURL string, want int) ([]boolqRow, error) {
	var rows []boolqRow
	for offset := 0; ; offset += boolqPageSize {
		if offset > 0 {
			time.Sleep(pageDelay)
		}
		data, err := getWithRetry(client, fmt.Sprintf("%s&offset=%d&length=%d", baseURL, offset, boolqPageSize))
		if err != nil {
			return nil, err
		}
		var page struct {
			Rows []struct {
				Row boolqRow `json:"row"`
			} `json:"rows"`
			Total int `json:"num_rows_total"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, fmt.Errorf("offset %d: %w", offset, err)
		}
		for _, r := range page.Rows {
			rows = append(rows, r.Row)
		}
		if len(page.Rows) == 0 || len(rows) >= page.Total {
			break
		}
	}
	if len(rows) != want {
		return nil, fmt.Errorf("expected %d BoolQ rows, got %d; the source has changed", want, len(rows))
	}
	return rows, nil
}

func fetchSMS(client *http.Client, url string, want int) ([]smsRow, error) {
	data, err := getWithRetry(client, url)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var rows []smsRow
	for _, f := range zr.File {
		if f.Name != "SMSSpamCollection" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
			label, text, ok := strings.Cut(line, "\t")
			if !ok || (label != "spam" && label != "ham") {
				return nil, fmt.Errorf("unexpected line %q", line)
			}
			rows = append(rows, smsRow{Spam: label == "spam", Text: text})
		}
	}
	if len(rows) != want {
		return nil, fmt.Errorf("expected %d SMS messages, got %d; the source has changed", want, len(rows))
	}
	return rows, nil
}
