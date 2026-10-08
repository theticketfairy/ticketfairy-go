package ticketfairy

import (
	"errors"
	"fmt"
	"time"
)

// The kinds of failure. Test for one with errors.Is:
//
//	if errors.Is(err, ticketfairy.ErrNotFound) { ... }
//
// and read the details with errors.As and *Error.
var (
	// ErrNetwork means no response arrived: the connection failed or timed out.
	ErrNetwork = errors.New("ticketfairy: could not reach Ticket Fairy")
	// ErrAuthentication means the token is missing, expired or revoked (401).
	ErrAuthentication = errors.New("ticketfairy: authentication failed")
	// ErrPermissionDenied means the token's person does not have the role this needs (403).
	ErrPermissionDenied = errors.New("ticketfairy: permission denied")
	// ErrNotFound means there is no such event, run or other resource (404).
	ErrNotFound = errors.New("ticketfairy: not found")
	// ErrConflict means the request clashes with earlier work, such as a reused key (409 or 410).
	ErrConflict = errors.New("ticketfairy: conflict")
	// ErrValidation means a parameter or field is not valid; the message names it (400 or 422).
	ErrValidation = errors.New("ticketfairy: validation failed")
	// ErrRateLimited means too many requests. Wait Error.RetryAfter before trying again (429).
	ErrRateLimited = errors.New("ticketfairy: rate limited")
	// ErrServer means Ticket Fairy could not complete the request; try again shortly (5xx).
	ErrServer = errors.New("ticketfairy: server error")
	// ErrUnexpectedResponse means a response did not have the shape this client expects.
	ErrUnexpectedResponse = errors.New("ticketfairy: unexpected response")
	// ErrSetupCopyTimeout means a setup copy was still running when Wait gave up.
	// The copy carries on; call Wait again to keep following it.
	ErrSetupCopyTimeout = errors.New("ticketfairy: setup copy still running")
)

// Error is any failure talking to the Ticket Fairy API.
type Error struct {
	// Kind is one of the Err values above, for errors.Is.
	Kind error
	// Status is the HTTP status, or 0 when no response arrived.
	Status int
	// Code is the API's stable error code, when it sent one.
	Code string
	// Message is the API's own message, or a description of the failure.
	Message string
	// Body is the parsed response body: a map, a slice, a string or nil.
	Body any
	// RequestID is the X-Request-Id response header, when present.
	RequestID string
	// RetryAfter is how long the server asked to wait, on a 429 or 503.
	RetryAfter time.Duration
	// Run is the setup copy's last state, for ErrSetupCopyTimeout.
	Run *Run

	cause error
}

func (e *Error) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("ticketfairy: %s (HTTP %d)", e.Message, e.Status)
	}
	return "ticketfairy: " + e.Message
}

// Unwrap returns the kind and, for a network error, the underlying failure.
func (e *Error) Unwrap() []error {
	errs := []error{}
	if e.Kind != nil {
		errs = append(errs, e.Kind)
	}
	if e.cause != nil {
		errs = append(errs, e.cause)
	}
	return errs
}

// Hint is what to do next, when the API says.
func (e *Error) Hint() string {
	if body, ok := e.Body.(map[string]any); ok {
		if hint, ok := body["hint"].(string); ok {
			return hint
		}
	}
	return ""
}

func kindForStatus(status int) error {
	switch {
	case status == 429:
		return ErrRateLimited
	case status == 401:
		return ErrAuthentication
	case status == 403:
		return ErrPermissionDenied
	case status == 404:
		return ErrNotFound
	case status == 409 || status == 410:
		return ErrConflict
	case status == 400 || status == 422:
		return ErrValidation
	case status >= 500:
		return ErrServer
	default:
		return nil
	}
}

func unexpected(what string, status int, body any) *Error {
	return &Error{
		Kind:    ErrUnexpectedResponse,
		Status:  status,
		Message: "Ticket Fairy returned an unexpected response for " + what + ".",
		Body:    body,
	}
}
