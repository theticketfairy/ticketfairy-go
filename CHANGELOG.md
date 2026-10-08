# Changelog

## 0.1.0

The first release.

- `Events.List` and `Events.All` read the public event listing, with every filter and cursor paging.
- `Events.Create` creates an event for a brand you manage, with an `Idempotency-Key`.
- `Events.Sales` reads an event's sales by day and by ticket type and release.
- `SetupCopy.Start`, `Get` and `Wait` copy another event's setup and follow the run.
- Errors are `*ticketfairy.Error` values with a kind for `errors.Is`.
- Retries follow the same rule as the npm and PyPI packages, and redirects are never followed.
