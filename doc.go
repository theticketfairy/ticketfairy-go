// Package ticketfairy is the Go client for the Ticket Fairy API.
//
// Use it to read public event listings, create events for a brand you
// manage, read an event's sales, and copy another event's setup into an
// event as a background job you can follow.
//
// Two APIs, one client:
//
//   - The public event listing on the consumer site, www.ticketfairy.com.
//     It needs no credential.
//   - The organiser API on the dashboard's domain, www.theticketfairy.com.
//     It needs a personal access token from your account settings, sent as
//     "Authorization: Bearer <token>".
//
// Both are described by OpenAPI documents: /api/v1/openapi.json on the
// consumer site and /api/openapi.json on the organiser API. The developer
// documentation is at https://www.ticketfairy.com/developers.
//
// Retries follow the same rule as the ticketfairy npm and PyPI packages, so a
// retry never runs a write twice. A network error, a 5xx or a 429 is retried
// only for a request that is safe to repeat: GET, HEAD, OPTIONS, PUT and
// DELETE, a write that carries an Idempotency-Key, or a write the server
// deduplicates by a key in its body. A 429 or 503 waits for Retry-After
// first.
//
// The package uses only the Go standard library.
package ticketfairy
