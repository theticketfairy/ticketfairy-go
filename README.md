# ticketfairy-go

The Go client for the [Ticket Fairy](https://www.ticketfairy.com) API. Use it to:

- read public event listings;
- create events for a brand you manage;
- read an event's sales;
- copy another event's setup into an event, as a background job you can follow.

It uses only the Go standard library and needs Go 1.23 or later.

```sh
go get github.com/theticketfairy/ticketfairy-go
```

[![Go Reference](https://pkg.go.dev/badge/github.com/theticketfairy/ticketfairy-go.svg)](https://pkg.go.dev/github.com/theticketfairy/ticketfairy-go)

The same API is available from Python in the [`ticketfairy` PyPI package](https://pypi.org/project/ticketfairy/), and from Node.js and the command line in the [`ticketfairy` npm package](https://www.npmjs.com/package/ticketfairy).

## Public event listings

The public listing needs no account.

```go
client := ticketfairy.New()

page, err := client.Events.List(ctx, ticketfairy.ListParams{Country: "GB", From: "2026-11-01", Size: 50})
if err != nil {
	return err
}
for _, event := range page.Events {
	fmt.Println(event.DisplayName, event.StartDate, event.URL)
}

// Every matching event, page after page:
for event, err := range client.Events.All(ctx, ticketfairy.ListParams{Search: "jazz"}, 200) {
	if err != nil {
		return err
	}
	fmt.Println(event.DisplayName)
}
```

`ListParams` takes these filters:

| Filter | Meaning |
| --- | --- |
| `Search` | Words to match against event names and descriptions |
| `Country` | An ISO 3166-1 alpha-2 country code |
| `State` | A region, state or province |
| `From`, `To` | Dates as `YYYY-MM-DD` |
| `SectionType` | The listing section to read |
| `Timezone` | An IANA timezone for the date window |
| `Sort` | `created_at`, `updated_at` or `start_date` |
| `Order` | `asc` or `desc` |
| `BrandID` | Only events from this brand |
| `Include` | The kinds of event to include |
| `Size` | Events per page, up to 200 |

`All` takes the same filters and follows `Pagination.NextCursor` for you. Stop ranging at any time to stop fetching.

Each `Event` has typed fields for what the listing sends, and `Raw` holds the event exactly as it arrived, including any field added later.

## Organiser API

The organiser API acts as you. To get a token:

1. Create a personal access token in your Ticket Fairy account settings.
2. Pass it with `ticketfairy.WithToken`, or set it in the `TICKETFAIRY_TOKEN` environment variable.

The token can do what your roles allow, and nothing more.

```go
client := ticketfairy.New(ticketfairy.WithToken("...")) // or set TICKETFAIRY_TOKEN

// Create a draft event for a brand where you are admin or owner.
event, err := client.Events.Create(ctx, ticketfairy.CreateParams{
	BrandID: 1234,
	Attributes: map[string]any{
		"displayName": "Summer Festival 2027",
		"slug":        "summer-festival-2027",
		"flagDraft":   true,
	},
})
if err != nil {
	return err
}
fmt.Println(event["id"])

// Tickets sold and revenue, by day and by ticket type and release.
sales, err := client.Events.Sales(ctx, 5678)
```

`Create` sends an `Idempotency-Key` with each request. If a network error happens, a retry cannot create a second event.

### Copy another event's setup

The copy runs in the background. `Start` returns at once with the run. `Wait` follows the run until it stops.

```go
run, err := client.SetupCopy.Start(ctx, newEventID, ticketfairy.StartParams{SourceEventID: lastYearEventID})
if err != nil {
	return err
}
run, err = client.SetupCopy.Wait(ctx, newEventID, run, ticketfairy.WaitOptions{Timeout: 10 * time.Minute})
if err != nil {
	return err
}
fmt.Println(run.Status) // done, partly_done or failed
for _, part := range run.Parts {
	fmt.Println(part.Label, part.Copied, part.Kept, part.LeftOut)
}
```

How a copy works:

- Both events must belong to the same brand.
- You need the owner, admin or producer role on both events.
- Forms need the owner or admin role.
- Leave out `Parts` to copy everything, or name the parts you want.

`Start` sends a request key. Sending the same key and the same choice of parts again returns the same run, so a retried start does not copy twice.

## Errors

Every failure from the API is an `*ticketfairy.Error`. Test its kind with `errors.Is`, and read the details with `errors.As`:

```go
_, err := client.Events.Sales(ctx, 5678)
var apiErr *ticketfairy.Error
if errors.Is(err, ticketfairy.ErrRateLimited) && errors.As(err, &apiErr) {
	time.Sleep(apiErr.RetryAfter)
}
```

An `*Error` has these fields:

- `Status`: the HTTP status, or 0 when no response arrived.
- `Code`: a stable code, when the API sends one.
- `Message`: the API's own explanation.
- `Hint()`: what to do next, when the API says.
- `Body`: the response.
- `RetryAfter`: how long the server asked to wait.

| Kind | When |
| --- | --- |
| `ErrAuthentication` | The token is missing, expired or revoked |
| `ErrPermissionDenied` | Your role does not allow this |
| `ErrNotFound` | No such event or run |
| `ErrValidation` | A parameter or field is not valid; the message names it |
| `ErrConflict` | A key was already used for something else, or a copy is already running |
| `ErrRateLimited` | Too many requests; wait `RetryAfter` |
| `ErrServer` | Ticket Fairy could not complete the request |
| `ErrNetwork` | No response arrived |
| `ErrUnexpectedResponse` | A response did not have the expected shape |
| `ErrSetupCopyTimeout` | `Wait` gave up while the copy was still running; the copy carries on |

### When the client retries

Reads are retried after a rate limit, a server error or a network error. For a rate limit, the client first waits for the `Retry-After` time.

Writes are retried in the same cases only when the API can tell a repeat from a new request. That is the case for `Create`, which sends an `Idempotency-Key`, and for `SetupCopy.Start`, which sends a request key. A repeat sends the same key, so it returns the first attempt's result rather than doing the work twice.

When the retries run out, the error is returned. Change the number of retries with `WithMaxRetries` and the longest wait with `WithMaxRetryWait`. Every call takes a `context.Context`, which also cancels a wait between retries.

The client never follows a redirect, so the token is only ever sent to the address you configured.

## API reference

The client follows these OpenAPI documents:

- [Public listing](https://www.ticketfairy.com/api/v1/openapi.json)
- [Organiser API](https://www.theticketfairy.com/api/openapi.json)

The developer guide is at [ticketfairy.com/developers](https://www.ticketfairy.com/developers).

## Licence

MIT. See [LICENSE](LICENSE).
