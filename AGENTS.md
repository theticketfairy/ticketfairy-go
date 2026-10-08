# ticketfairy-go: rules for coding agents

This is the Go client for the Ticket Fairy API. Rules for writing code that
uses Ticket Fairy are in https://github.com/theticketfairy/agent-skills/blob/main/AGENTS.md.

When you change this module:

- Keep it to the Go standard library.
- Keep the retry rule the same as the npm and PyPI packages: a write is
  retried only when it carries an Idempotency-Key or a key the server
  deduplicates by. Never follow a redirect.
- Never print or log the token.
- Run `gofmt -l .`, `go vet ./...` and `go test -race ./...` before you commit.
- Bump `Version` in client.go and add a CHANGELOG entry for each release, then
  tag it `vX.Y.Z`.
