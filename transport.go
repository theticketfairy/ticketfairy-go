package ticketfairy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// request is one call to the API.
type request struct {
	method  string
	path    string
	url     string
	query   url.Values
	body    any
	headers map[string]string
	// safeToRepeat marks a write the server deduplicates by a key in its body.
	safeToRepeat bool
}

// response is a parsed HTTP response.
type response struct {
	status  int
	headers http.Header
	// body is the JSON body decoded with UseNumber, the raw text when it is
	// not JSON, or nil when it is empty.
	body any
	raw  []byte
}

type transport struct {
	httpClient   *http.Client
	userAgent    string
	maxRetries   int
	maxRetryWait time.Duration
	sleep        func(context.Context, time.Duration) error
}

// prepare stops the HTTP client following redirects. The API never redirects,
// and following one would send the token to another address.
func (t *transport) prepare() {
	copied := *t.httpClient
	copied.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	t.httpClient = &copied
}

func (t *transport) do(ctx context.Context, r request) (*response, error) {
	method := strings.ToUpper(r.method)
	target := r.url
	if encoded := r.query.Encode(); encoded != "" {
		separator := "?"
		if strings.Contains(target, "?") {
			separator = "&"
		}
		target += separator + encoded
	}
	var payload []byte
	if r.body != nil {
		var err error
		if payload, err = json.Marshal(r.body); err != nil {
			return nil, err
		}
	}
	headers := map[string]string{"User-Agent": t.userAgent, "Accept": "application/json"}
	for name, value := range r.headers {
		headers[name] = value
	}
	if payload != nil {
		if _, ok := headers["Content-Type"]; !ok {
			headers["Content-Type"] = "application/json"
		}
	}
	_, hasKey := headers["Idempotency-Key"]
	repeatable := r.safeToRepeat || hasKey || isSafeMethod(method)

	for attempt := 0; ; attempt++ {
		res, err := t.send(ctx, method, target, payload, headers)
		if err != nil {
			if ctx.Err() == nil && repeatable && attempt < t.maxRetries {
				if waitErr := t.sleep(ctx, backoff(attempt)); waitErr != nil {
					return nil, waitErr
				}
				continue
			}
			return nil, err
		}
		if res.status >= 200 && res.status < 300 {
			return res, nil
		}
		retryAfter, hasRetryAfter := parseRetryAfter(res.headers.Get("Retry-After"), time.Now())
		if !hasRetryAfter && res.status == http.StatusTooManyRequests {
			// The API's own rate limiter puts the wait in the body, not the header.
			retryAfter, hasRetryAfter = bodyRetryAfter(res.body)
		}
		if (res.status == http.StatusTooManyRequests || res.status >= 500) && repeatable && attempt < t.maxRetries {
			// A Retry-After on a 429 or a load-shedding 503 replaces the backoff.
			wait := backoff(attempt)
			if hasRetryAfter {
				wait = retryAfter
			}
			wait = min(max(wait, 500*time.Millisecond), t.maxRetryWait)
			if waitErr := t.sleep(ctx, wait); waitErr != nil {
				return nil, waitErr
			}
			continue
		}
		message, code := describe(res)
		return nil, &Error{
			Kind:       kindForStatus(res.status),
			Status:     res.status,
			Code:       code,
			Message:    message,
			Body:       res.body,
			RequestID:  res.headers.Get("X-Request-Id"),
			RetryAfter: retryAfter,
		}
	}
}

func (t *transport) send(ctx context.Context, method, target string, payload []byte, headers map[string]string) (*response, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	res, err := t.httpClient.Do(req)
	if err != nil {
		return nil, networkError(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		// The body was cut off after the headers.
		return nil, networkError(err)
	}
	return &response{status: res.StatusCode, headers: res.Header, body: decodeBody(raw), raw: raw}, nil
}

func networkError(err error) *Error {
	return &Error{Kind: ErrNetwork, Message: "Could not reach Ticket Fairy: " + err.Error(), cause: err}
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete:
		return true
	}
	return false
}

func backoff(attempt int) time.Duration {
	seconds := math.Min(0.5*math.Pow(2, float64(attempt)), 8)
	return time.Duration(seconds * float64(time.Second))
}

func sleepContext(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func decodeBody(raw []byte) any {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var body any
	if err := decoder.Decode(&body); err != nil {
		return string(raw)
	}
	return body
}

// parseRetryAfter reads either form of Retry-After: seconds or an HTTP date.
func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		return time.Duration(math.Max(0, seconds) * float64(time.Second)), true
	}
	if when, err := http.ParseTime(value); err == nil {
		return max(0, when.Sub(now)), true
	}
	return 0, false
}

func bodyRetryAfter(body any) (time.Duration, bool) {
	fields, ok := body.(map[string]any)
	if !ok {
		return 0, false
	}
	number, ok := fields["retry_after"].(json.Number)
	if !ok {
		return 0, false
	}
	seconds, err := number.Float64()
	if err != nil {
		return 0, false
	}
	return time.Duration(math.Max(0, seconds) * float64(time.Second)), true
}

// describe finds the API's own message and code in any of its error shapes.
func describe(res *response) (string, string) {
	message, code := "", ""
	switch body := res.body.(type) {
	case map[string]any:
		if text, ok := body["message"].(string); ok {
			message = text
		} else if text, ok := body["error"].(string); ok {
			message = text
		}
		if errs, ok := body["errors"].([]any); ok && message == "" && len(errs) > 0 {
			if first, ok := errs[0].(map[string]any); ok {
				if text, ok := first["detail"].(string); ok && text != "" {
					message = text
				} else if text, ok := first["title"].(string); ok {
					message = text
				}
				if text, ok := first["code"].(string); ok {
					code = text
				}
			}
		}
		if text, ok := body["code"].(string); ok {
			code = text
		}
	case string:
		if trimmed := strings.TrimSpace(body); trimmed != "" && len(body) <= 500 {
			message = trimmed
		}
	}
	if message == "" {
		message = "Ticket Fairy answered HTTP " + strconv.Itoa(res.status) + "."
	}
	return message, code
}
