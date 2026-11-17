package ask

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const attempts = 3

var retryDelay = 300 * time.Millisecond

// postJSON retries connection errors and 5xx responses, which can be a server
// that is momentarily busy. A 4xx response is a bad request and fails at once.
func postJSON(ctx context.Context, client *http.Client, url string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var last error
	for i := 1; i <= attempts; i++ {
		retry, err := postOnce(ctx, client, url, payload, out)
		if err == nil {
			return nil
		}
		last = err
		if !retry || ctx.Err() != nil || i == attempts {
			break
		}
		select {
		case <-time.After(retryDelay * time.Duration(i)):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return last
}

func postOnce(ctx context.Context, client *http.Client, url string, payload []byte, out any) (retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return true, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return true, err
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode >= 500, fmt.Errorf("%s returned %d: %s", url, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return false, json.Unmarshal(data, out)
}
