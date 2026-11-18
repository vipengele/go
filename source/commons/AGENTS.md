# commons

`go.vipengele.org/commons`: generic building blocks every library of the platform may require. It
requires no sibling project and nothing beyond the standard library. Read `doc.go` first, then the
`doc.go` of each package (`clock`, `envvar`, `validate`).

## Commands (run from `source/commons`)

```bash
go build ./...
go vet ./...
go test -race -shuffle=on ./...
golangci-lint run
```

## Clock

Code that sleeps or defers a call takes a `clock.Clock` and never calls `time.Sleep` or
`time.AfterFunc` itself, so a test can pass `clocktest.New` and advance time by hand (ADR-0040).
Production code passes `clock.System()`. A test driving a sleep in another goroutine polls
`Pending()` for the sleep to register before calling `Advance`.
