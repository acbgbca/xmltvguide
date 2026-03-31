# Review: `time.Tick` in a Goroutine Leaks the Underlying Ticker

## Problem

[main.go lines 72–78](../main.go#L72) starts the background refresh loop using `time.Tick`:

```go
go func() {
    for range time.Tick(pollInterval) {
        if err := refresh(db, xmltvURL, pollInterval); err != nil {
            log.Printf("refresh error: %v", err)
        }
    }
}()
```

## Why It Is an Issue

`time.Tick` is a convenience wrapper around `time.NewTicker` that **deliberately discards the `*Ticker`**. From the standard library documentation:

> The underlying Ticker cannot be stopped. If efficiency is a concern, use `NewTicker` instead and call `Stop` when the ticker is no longer needed.

Because the ticker can never be stopped, the goroutine and its channel will live for the entire lifetime of the process with no way to clean them up. This has two practical consequences:

### 1. No graceful shutdown

When the application receives a shutdown signal (SIGTERM from Docker, Kubernetes, or `docker stop`), there is currently no graceful shutdown path at all — `http.ListenAndServe` blocks forever and the process is killed. If a graceful shutdown were added in future (which it should be — see review 05), the background goroutine and its ticker could not be stopped because the ticker handle is discarded. The goroutine and channel would be leaked until the process exits.

### 2. Go vet and linters flag this pattern

`go vet` does not catch this specific case, but `staticcheck` (the de-facto Go linter) raises `SA1015: using time.Tick in a non-main function leaks the underlying ticker`. This makes the codebase fail standard static analysis pipelines.

### 3. It signals unfamiliarity with the `time` package conventions

The Go documentation explicitly calls out `time.Tick` as a tool for `main` functions or other code where the leak "doesn't matter". Using it in production long-running code is a known Go anti-pattern.

## What Should Be Done Instead

Use `time.NewTicker`, retain the `*Ticker` handle, and call `Stop()` when the goroutine should exit:

```go
ticker := time.NewTicker(pollInterval)
go func() {
    defer ticker.Stop()
    for range ticker.C {
        if err := refresh(db, xmltvURL, pollInterval); err != nil {
            log.Printf("refresh error: %v", err)
        }
    }
}()
```

When graceful shutdown is added, `ticker.Stop()` can be called from the shutdown path (e.g., via a `context.Context` cancellation) to cleanly terminate the goroutine.

## References

- [`time.Tick` documentation](https://pkg.go.dev/time#Tick) — "The underlying Ticker cannot be stopped."
- [`time.NewTicker` documentation](https://pkg.go.dev/time#NewTicker)
- [Staticcheck SA1015](https://staticcheck.dev/docs/checks/#SA1015)
- [Effective Go — goroutines](https://go.dev/doc/effective_go#goroutines)
