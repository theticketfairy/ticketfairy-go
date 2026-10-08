package ticketfairy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder is a test server that answers each request with the next handler
// and records what it received.
type recorder struct {
	t        *testing.T
	mu       sync.Mutex
	requests []*http.Request
	bodies   []string
	handlers []http.HandlerFunc
	server   *httptest.Server
}

func newRecorder(t *testing.T, handlers ...http.HandlerFunc) *recorder {
	t.Helper()
	r := &recorder{t: t, handlers: handlers}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		index := len(r.requests)
		body, _ := io.ReadAll(req.Body)
		r.requests = append(r.requests, req)
		r.bodies = append(r.bodies, string(body))
		r.mu.Unlock()
		if index >= len(r.handlers) {
			t.Errorf("unexpected request %d: %s %s", index+1, req.Method, req.URL)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		r.handlers[index](w, req)
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func jsonReply(status int, body string, headers ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		for i := 0; i+1 < len(headers); i += 2 {
			w.Header().Set(headers[i], headers[i+1])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// noSleep records the waits instead of sleeping.
type noSleep struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (n *noSleep) sleep(ctx context.Context, wait time.Duration) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.waits = append(n.waits, wait)
	return ctx.Err()
}

func testClient(r *recorder, sleeper *noSleep, options ...Option) *Client {
	t := r.t
	t.Setenv("TICKETFAIRY_TOKEN", "")
	all := append([]Option{
		WithPublicBaseURL(r.server.URL),
		WithOrganiserBaseURL(r.server.URL + "/"),
		withSleep(sleeper.sleep),
	}, options...)
	return New(all...)
}

const listingPage1 = `{"data":{"events":[{"url":"https://www.ticketfairy.com/event/a","displayName":"A","startDate":"2027-01-10T12:00:00+11:00","timezone":"Australia/Melbourne","startingPrice":{"amount":"49.00","currency":"AUD"},"currency":{"currency":"AUD","symbol":"$","decimal_places":"2"},"venue":{"name":null,"city":"Melbourne","country":"au","state":"VIC","hidden":true},"newField":42}],"pagination":{"page":1,"size":1,"totalCount":3,"totalPages":3,"nextCursor":"c2"}},"success":true,"error":false,"message":"ok","status":200}`
const listingPage2 = `{"data":{"events":[{"displayName":"B"},{"displayName":"C"}],"pagination":{"page":2,"size":2,"totalCount":3,"totalPages":2,"nextCursor":null}},"success":true,"error":false,"message":"ok","status":200}`

func TestListSendsFiltersAndDecodesEvents(t *testing.T) {
	r := newRecorder(t, jsonReply(200, listingPage1))
	client := testClient(r, &noSleep{})

	page, err := client.Events.List(context.Background(), ListParams{
		Search: "jazz club", Country: "GB", From: "2026-11-01", SectionType: "none",
		Timezone: "Europe/London", BrandID: 7, Include: []string{"paid_events", "free_events"}, Size: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := r.requests[0]
	if req.URL.Path != "/api/v1/events/listing" {
		t.Errorf("path = %s", req.URL.Path)
	}
	want := map[string]string{"search": "jazz club", "country": "GB", "from": "2026-11-01", "section_type": "none",
		"timezone": "Europe/London", "brandId": "7", "include": "paid_events,free_events", "size": "50"}
	for name, value := range want {
		if got := req.URL.Query().Get(name); got != value {
			t.Errorf("query %s = %q, want %q", name, got, value)
		}
	}
	for _, absent := range []string{"q", "page", "cursor", "to", "state"} {
		if req.URL.Query().Has(absent) {
			t.Errorf("query has %s", absent)
		}
	}
	if req.Header.Get("Authorization") != "" {
		t.Error("the public listing must not send a token")
	}
	if !strings.HasPrefix(req.Header.Get("User-Agent"), "ticketfairy-go/") {
		t.Errorf("User-Agent = %q", req.Header.Get("User-Agent"))
	}
	if len(page.Events) != 1 || page.Pagination.NextCursor != "c2" || page.Pagination.TotalCount != 3 {
		t.Fatalf("page = %+v", page)
	}
	event := page.Events[0]
	if event.DisplayName != "A" || event.StartingPrice.Amount != "49.00" || event.Venue.City != "Melbourne" || !event.Venue.Hidden {
		t.Errorf("event = %+v", event)
	}
	if string(event.Currency.DecimalPlaces) != `"2"` {
		t.Errorf("decimal places = %s", event.Currency.DecimalPlaces)
	}
	if !strings.Contains(string(event.Raw), `"newField":42`) {
		t.Errorf("Raw lost an unknown field: %s", event.Raw)
	}
}

func TestAllFollowsTheCursorAndStopsAtTheLimit(t *testing.T) {
	r := newRecorder(t, jsonReply(200, listingPage1), jsonReply(200, listingPage2))
	client := testClient(r, &noSleep{})

	var names []string
	for event, err := range client.Events.All(context.Background(), ListParams{Search: "x", Page: 9, Cursor: "ignored"}, 0) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, event.DisplayName)
	}
	if strings.Join(names, ",") != "A,B,C" {
		t.Errorf("names = %v", names)
	}
	if r.requests[0].URL.Query().Has("cursor") || r.requests[0].URL.Query().Has("page") {
		t.Error("the first page must not send a cursor or page")
	}
	if got := r.requests[1].URL.Query(); got.Get("cursor") != "c2" || got.Get("search") != "x" {
		t.Errorf("second page query = %v", got)
	}

	r2 := newRecorder(t, jsonReply(200, listingPage1), jsonReply(200, listingPage2))
	limited := testClient(r2, &noSleep{})
	count := 0
	for _, err := range limited.Events.All(context.Background(), ListParams{}, 2) {
		if err != nil {
			t.Fatal(err)
		}
		count++
	}
	if count != 2 {
		t.Errorf("limit 2 yielded %d", count)
	}

	// Stopping the loop stops fetching.
	r3 := newRecorder(t, jsonReply(200, listingPage1))
	stopped := testClient(r3, &noSleep{})
	for range stopped.Events.All(context.Background(), ListParams{}, 0) {
		break
	}
	if r3.count() != 1 {
		t.Errorf("breaking out fetched %d pages", r3.count())
	}
}

func TestReadsAreRetriedAndHonourRetryAfter(t *testing.T) {
	r := newRecorder(t,
		jsonReply(503, `{"message":"busy"}`, "Retry-After", "7"),
		jsonReply(429, `{"message":"slow down","retry_after":3}`),
		jsonReply(200, listingPage2),
	)
	sleeper := &noSleep{}
	client := testClient(r, sleeper)

	if _, err := client.Events.List(context.Background(), ListParams{}); err != nil {
		t.Fatal(err)
	}
	if r.count() != 3 {
		t.Fatalf("attempts = %d", r.count())
	}
	if fmt.Sprint(sleeper.waits) != "[7s 3s]" {
		t.Errorf("waits = %v", sleeper.waits)
	}
}

func TestRetryWaitIsCappedAndRetriesRunOut(t *testing.T) {
	r := newRecorder(t,
		jsonReply(503, `{}`, "Retry-After", "600"),
		jsonReply(503, `{"message":"still busy"}`),
	)
	sleeper := &noSleep{}
	client := testClient(r, sleeper, WithMaxRetries(1), WithMaxRetryWait(5*time.Second))

	_, err := client.Events.List(context.Background(), ListParams{})
	if !errors.Is(err, ErrServer) {
		t.Fatalf("err = %v", err)
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != 503 || apiErr.Message != "still busy" {
		t.Errorf("error = %#v", apiErr)
	}
	if fmt.Sprint(sleeper.waits) != "[5s]" {
		t.Errorf("waits = %v", sleeper.waits)
	}
}

func TestAWriteWithoutAKeyIsNeverRetried(t *testing.T) {
	r := newRecorder(t, jsonReply(503, `{"message":"busy"}`))
	client := testClient(r, &noSleep{})
	_, err := client.transport.do(context.Background(), request{method: "POST", url: r.server.URL + "/x", body: map[string]any{}})
	if !errors.Is(err, ErrServer) || r.count() != 1 {
		t.Fatalf("err = %v, attempts = %d", err, r.count())
	}
}

func TestCreateSendsJSONAPIWithAnIdempotencyKeyAndRetries(t *testing.T) {
	created := `{"data":{"id":"901","type":"event","attributes":{"displayName":"Launch","slug":"launch","flagDraft":true}}}`
	r := newRecorder(t, jsonReply(503, `{}`), jsonReply(201, created))
	client := testClient(r, &noSleep{}, WithToken("secret-token"))

	event, err := client.Events.Create(context.Background(), CreateParams{
		BrandID:    12,
		Attributes: map[string]any{"displayName": "Launch", "slug": "launch", "flagDraft": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if event["id"] != "901" || event["slug"] != "launch" || event["type"] != "event" {
		t.Errorf("event = %v", event)
	}
	first, second := r.requests[0], r.requests[1]
	key := first.Header.Get("Idempotency-Key")
	if len(key) != 36 || key != second.Header.Get("Idempotency-Key") {
		t.Errorf("keys = %q, %q", key, second.Header.Get("Idempotency-Key"))
	}
	if first.Header.Get("Authorization") != "Bearer secret-token" || first.Header.Get("Content-Type") != jsonAPI || first.Header.Get("Accept") != jsonAPI {
		t.Errorf("headers = %v", first.Header)
	}
	if first.URL.Path != "/api/events" || first.Method != "POST" {
		t.Errorf("request = %s %s", first.Method, first.URL.Path)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(r.bodies[0]), &body); err != nil {
		t.Fatal(err)
	}
	owner := body["data"].(map[string]any)["relationships"].(map[string]any)["owner"].(map[string]any)["data"].(map[string]any)
	if owner["id"] != "12" || owner["type"] != "brand" {
		t.Errorf("owner = %v", owner)
	}

	r2 := newRecorder(t, jsonReply(201, created))
	keyed := testClient(r2, &noSleep{}, WithToken("t"))
	if _, err := keyed.Events.Create(context.Background(), CreateParams{BrandID: 1, IdempotencyKey: "mine"}); err != nil {
		t.Fatal(err)
	}
	if r2.requests[0].Header.Get("Idempotency-Key") != "mine" {
		t.Error("the caller's key must be sent")
	}
}

func TestOrganiserCallsNeedAToken(t *testing.T) {
	r := newRecorder(t)
	client := testClient(r, &noSleep{})
	_, err := client.Events.Sales(context.Background(), 5)
	if !errors.Is(err, ErrAuthentication) || r.count() != 0 {
		t.Fatalf("err = %v, requests = %d", err, r.count())
	}
	if !strings.Contains(err.Error(), "TICKETFAIRY_TOKEN") {
		t.Errorf("message = %s", err)
	}

	t.Setenv("TICKETFAIRY_TOKEN", "from-env")
	if !New().Authenticated() {
		t.Error("TICKETFAIRY_TOKEN must be read")
	}
}

func TestSalesSendsTheSections(t *testing.T) {
	r := newRecorder(t, jsonReply(200, `{"data":{"sales":[{"date":"2026-10-01"}],"release_breakdown":[]},"success":true}`),
		jsonReply(200, `{"data":{"sales":[]}}`))
	client := testClient(r, &noSleep{}, WithToken("t"))

	sales, err := client.Events.Sales(context.Background(), 77)
	if err != nil {
		t.Fatal(err)
	}
	if r.requests[0].URL.Path != "/api/events/77/relationships/performance" || r.requests[0].URL.Query().Get("section") != "sales|release_breakdown" {
		t.Errorf("request = %s", r.requests[0].URL)
	}
	if len(sales["sales"].([]any)) != 1 {
		t.Errorf("sales = %v", sales)
	}
	if _, err := client.Events.Sales(context.Background(), 77, "sales"); err != nil {
		t.Fatal(err)
	}
	if r.requests[1].URL.Query().Get("section") != "sales" {
		t.Errorf("sections = %s", r.requests[1].URL.Query().Get("section"))
	}
}

func TestErrorsCarryKindStatusCodeAndHint(t *testing.T) {
	cases := []struct {
		status int
		body   string
		kind   error
		msg    string
		code   string
	}{
		{401, `{"message":"Unauthorized","code":"unauthorized","hint":"Send a token."}`, ErrAuthentication, "Unauthorized", "unauthorized"},
		{403, `{"error":"No role"}`, ErrPermissionDenied, "No role", ""},
		{404, `{"errors":[{"detail":"No such event","code":"not_found"}]}`, ErrNotFound, "No such event", "not_found"},
		{409, `{"message":"Key reused"}`, ErrConflict, "Key reused", ""},
		{422, `{"errors":[{"title":"slug is taken"}]}`, ErrValidation, "slug is taken", ""},
		{418, `short and plain`, nil, "short and plain", ""},
	}
	for _, c := range cases {
		r := newRecorder(t, jsonReply(c.status, c.body, "X-Request-Id", "req-1"))
		client := testClient(r, &noSleep{}, WithToken("t"))
		_, err := client.Events.Sales(context.Background(), 1)
		var apiErr *Error
		if !errors.As(err, &apiErr) {
			t.Fatalf("%d: err = %v", c.status, err)
		}
		if c.kind != nil && !errors.Is(err, c.kind) {
			t.Errorf("%d: kind = %v", c.status, apiErr.Kind)
		}
		if apiErr.Status != c.status || apiErr.Message != c.msg || apiErr.Code != c.code || apiErr.RequestID != "req-1" {
			t.Errorf("%d: error = %+v", c.status, apiErr)
		}
	}

	r := newRecorder(t, jsonReply(401, `{"message":"Unauthorized","hint":"Send a token."}`))
	_, err := testClient(r, &noSleep{}, WithToken("t")).Events.Sales(context.Background(), 1)
	if apiErr, _ := err.(*Error); apiErr == nil || apiErr.Hint() != "Send a token." {
		t.Errorf("hint missing: %v", err)
	}
}

func TestRateLimitErrorCarriesRetryAfter(t *testing.T) {
	r := newRecorder(t, jsonReply(429, `{"message":"Too many"}`, "Retry-After", "12"))
	client := testClient(r, &noSleep{}, WithMaxRetries(0))
	_, err := client.Events.List(context.Background(), ListParams{})
	var apiErr *Error
	if !errors.As(err, &apiErr) || !errors.Is(err, ErrRateLimited) || apiErr.RetryAfter != 12*time.Second {
		t.Fatalf("err = %v (%+v)", err, apiErr)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	var reached bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer elsewhere.Close()
	r := newRecorder(t, func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, elsewhere.URL, http.StatusFound)
	})
	client := testClient(r, &noSleep{}, WithToken("secret"), WithHTTPClient(&http.Client{}))
	_, err := client.Events.Sales(context.Background(), 1)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusFound || reached {
		t.Fatalf("err = %v, reached = %v", err, reached)
	}
}

func TestNetworkErrorsAreRetriedForReads(t *testing.T) {
	r := newRecorder(t)
	r.server.Close()
	sleeper := &noSleep{}
	client := testClient(r, sleeper)
	_, err := client.Events.List(context.Background(), ListParams{})
	if !errors.Is(err, ErrNetwork) {
		t.Fatalf("err = %v", err)
	}
	if len(sleeper.waits) != 2 || sleeper.waits[0] != 500*time.Millisecond || sleeper.waits[1] != time.Second {
		t.Errorf("waits = %v", sleeper.waits)
	}
}

func TestUnexpectedShapesAreReported(t *testing.T) {
	r := newRecorder(t, jsonReply(200, `{"data":"nope"}`))
	_, err := testClient(r, &noSleep{}).Events.List(context.Background(), ListParams{})
	if !errors.Is(err, ErrUnexpectedResponse) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheTokenIsNeverPrinted(t *testing.T) {
	client := New(WithToken("very-secret"))
	for _, text := range []string{client.String(), fmt.Sprintf("%v", client), fmt.Sprintf("%#v", client), fmt.Sprintf("%+v", client)} {
		if strings.Contains(text, "very-secret") {
			t.Errorf("token printed: %s", text)
		}
	}
}

func TestWithUserAgentPrefixesTheProduct(t *testing.T) {
	r := newRecorder(t, jsonReply(200, listingPage2))
	client := testClient(r, &noSleep{}, WithUserAgent("my-app/2.0"))
	if _, err := client.Events.List(context.Background(), ListParams{}); err != nil {
		t.Fatal(err)
	}
	if got := r.requests[0].Header.Get("User-Agent"); got != "my-app/2.0 ticketfairy-go/"+Version {
		t.Errorf("User-Agent = %q", got)
	}
}

func run(status string) string {
	return `{"data":{"id":31,"status":"` + status + `","source_event":{"event_id":5,"name":"Last year"},"parts":[{"key":"tickets","label":"Tickets","status":"done","copied":4,"kept":1,"left_out":0,"error_code":null}],"day_offset":364,"error_code":null}}`
}

func TestSetupCopyStartIsSafeToRetryAndWaitFollowsTheRun(t *testing.T) {
	r := newRecorder(t,
		jsonReply(503, `{}`),
		jsonReply(202, run(RunQueued)),
		jsonReply(200, run(RunRunning)),
		jsonReply(200, run(RunDone)),
	)
	sleeper := &noSleep{}
	client := testClient(r, sleeper, WithToken("t"))
	ctx := context.Background()

	started, err := client.SetupCopy.Start(ctx, 9, StartParams{SourceEventID: 5, Parts: []string{"tickets"}})
	if err != nil {
		t.Fatal(err)
	}
	if started.ID != 31 || started.Status != RunQueued || started.Final() {
		t.Fatalf("started = %+v", started)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(r.bodies[0]), &body)
	if body["source_event_id"] != float64(5) || len(body["request_key"].(string)) != 32 || r.bodies[0] != r.bodies[1] {
		t.Errorf("start body = %s then %s", r.bodies[0], r.bodies[1])
	}
	if r.requests[0].URL.Path != "/api/event/9/setup-copy/runs" {
		t.Errorf("path = %s", r.requests[0].URL.Path)
	}

	final, err := client.SetupCopy.Wait(ctx, 9, started, WaitOptions{Interval: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != RunDone || final.Parts[0].Copied != 4 || *final.DayOffset != 364 || final.SourceEvent.Name != "Last year" {
		t.Errorf("final = %+v", final)
	}
	if r.requests[2].URL.Path != "/api/event/9/setup-copy/runs/31" {
		t.Errorf("get path = %s", r.requests[2].URL.Path)
	}
}

func TestWaitGivesUpAtTheTimeoutWithTheLastState(t *testing.T) {
	r := newRecorder(t)
	client := New(WithToken("t"), WithOrganiserBaseURL(r.server.URL), withSleep(func(context.Context, time.Duration) error {
		time.Sleep(20 * time.Millisecond)
		return nil
	}))
	queued := &Run{ID: 31, Status: RunRunning}
	last, err := client.SetupCopy.Wait(context.Background(), 9, queued, WaitOptions{Timeout: 10 * time.Millisecond, Interval: time.Second})
	var apiErr *Error
	if !errors.Is(err, ErrSetupCopyTimeout) || !errors.As(err, &apiErr) || apiErr.Run != queued || last != queued {
		t.Fatalf("err = %v, last = %+v", err, last)
	}
	if r.count() != 0 {
		t.Errorf("a request was sent after the deadline")
	}
}

func TestParseRetryAfterReadsSecondsAndDates(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	if wait, ok := parseRetryAfter("2.5", now); !ok || wait != 2500*time.Millisecond {
		t.Errorf("seconds = %v %v", wait, ok)
	}
	if wait, ok := parseRetryAfter("Thu, 08 Oct 2026 12:00:30 GMT", now); !ok || wait != 30*time.Second {
		t.Errorf("date = %v %v", wait, ok)
	}
	if _, ok := parseRetryAfter("soon", now); ok {
		t.Error("garbage must not parse")
	}
}
