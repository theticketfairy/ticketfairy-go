package ticketfairy

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SetupCopyService copies another event's setup into an event, as a
// background job. Start answers at once with the run, and Wait follows it
// until it stops.
type SetupCopyService struct {
	client *Client
}

// The statuses of a setup copy run. Queued and running mean the copy is still
// going; done, partly done and failed are final.
const (
	RunQueued     = "queued"
	RunRunning    = "running"
	RunDone       = "done"
	RunPartlyDone = "partly_done"
	RunFailed     = "failed"
)

// Run is one setup copy. Raw holds the run exactly as it arrived, including
// any field this type does not name.
type Run struct {
	ID          int64           `json:"id"`
	Status      string          `json:"status"`
	SourceEvent *RunSource      `json:"source_event"`
	Parts       []RunPart       `json:"parts"`
	DayOffset   *int            `json:"day_offset"`
	ErrorCode   string          `json:"error_code"`
	Raw         json.RawMessage `json:"-"`
}

// Final reports whether the copy has stopped.
func (r *Run) Final() bool {
	switch r.Status {
	case RunDone, RunPartlyDone, RunFailed:
		return true
	}
	return false
}

// RunSource is the event a run copies from.
type RunSource struct {
	EventID int64  `json:"event_id"`
	Name    string `json:"name"`
}

// RunPart is one part of the setup chosen for a run, with how many items
// were copied, kept because the event already had them, and left out.
type RunPart struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Status    string `json:"status"`
	Copied    int    `json:"copied"`
	Kept      int    `json:"kept"`
	LeftOut   int    `json:"left_out"`
	ErrorCode string `json:"error_code"`
}

// StartParams describes a setup copy.
type StartParams struct {
	// SourceEventID is the event to copy from. Both events must belong to the
	// same brand, and you need the owner, admin or producer role on both.
	// Forms need the owner or admin role.
	SourceEventID int64
	// Parts are the parts of the setup to copy. Leave it empty to copy
	// everything.
	Parts []string
	// RequestKey makes the start safe to retry: the same key and choice
	// return the same run. A new random key is used when it is empty.
	RequestKey string
}

// Start starts copying params.SourceEventID's setup into eventID, and
// returns the run.
func (s *SetupCopyService) Start(ctx context.Context, eventID int64, params StartParams) (*Run, error) {
	key := params.RequestKey
	if key == "" {
		key = strings.ReplaceAll(newUUID(), "-", "")
	}
	body := map[string]any{"source_event_id": params.SourceEventID, "request_key": key}
	if params.Parts != nil {
		body["parts"] = params.Parts
	}
	res, err := s.client.organiser(ctx, request{
		method:       "POST",
		path:         "/api/event/" + strconv.FormatInt(eventID, 10) + "/setup-copy/runs",
		body:         body,
		headers:      map[string]string{"Content-Type": "application/json"},
		safeToRepeat: true,
	})
	if err != nil {
		return nil, err
	}
	return decodeRun(res)
}

// Get returns one run as it is now.
func (s *SetupCopyService) Get(ctx context.Context, eventID, runID int64) (*Run, error) {
	res, err := s.client.organiser(ctx, request{
		method: "GET",
		path:   "/api/event/" + strconv.FormatInt(eventID, 10) + "/setup-copy/runs/" + strconv.FormatInt(runID, 10),
	})
	if err != nil {
		return nil, err
	}
	return decodeRun(res)
}

// WaitOptions control how Wait follows a run.
type WaitOptions struct {
	// Timeout is how long to follow the run. The default is 10 minutes.
	Timeout time.Duration
	// Interval is the wait between reads. The default is 3 seconds.
	Interval time.Duration
}

// Wait follows a run until it stops, and returns its final state. If the run
// is still going after the timeout, Wait returns the run's last state and an
// *Error of kind ErrSetupCopyTimeout. The copy carries on, and you can call
// Wait again.
func (s *SetupCopyService) Wait(ctx context.Context, eventID int64, run *Run, options WaitOptions) (*Run, error) {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	interval := options.Interval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	deadline := time.Now().Add(timeout)
	current := run
	for !current.Final() {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return current, s.timedOut(current, timeout)
		}
		// Never sleep past the deadline.
		if err := s.client.transport.sleep(ctx, min(interval, remaining)); err != nil {
			return current, err
		}
		if !time.Now().Before(deadline) {
			// The deadline passed during the sleep, so do not start a request after it.
			return current, s.timedOut(current, timeout)
		}
		next, err := s.Get(ctx, eventID, current.ID)
		if err != nil {
			return current, err
		}
		current = next
	}
	return current, nil
}

func (s *SetupCopyService) timedOut(run *Run, timeout time.Duration) error {
	return &Error{
		Kind:    ErrSetupCopyTimeout,
		Message: fmt.Sprintf("The setup copy is still %s after %s. It carries on; call Wait again to keep following it.", run.Status, timeout),
		Body:    run.Raw,
		Run:     run,
	}
}

func decodeRun(res *response) (*Run, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(res.raw, &envelope); err != nil || len(envelope.Data) == 0 {
		return nil, unexpected("the setup copy", res.status, res.body)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Data, &probe); err != nil || probe["id"] == nil {
		return nil, unexpected("the setup copy", res.status, res.body)
	}
	run := &Run{}
	if err := json.Unmarshal(envelope.Data, run); err != nil {
		return nil, unexpected("the setup copy", res.status, res.body)
	}
	run.Raw = envelope.Data
	return run, nil
}
