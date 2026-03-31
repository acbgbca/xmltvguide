# Review: No Graceful Shutdown — In-Flight Requests and DB Writes Are Abandoned

## Problem

[main.go lines 91–93](../main.go#L91) starts the HTTP server with a bare `http.ListenAndServe`:

```go
log.Printf("TV Guide starting on :%s ...", port, ...)
log.Fatal(http.ListenAndServe(":"+port, mux))
```

There is no signal handling and no graceful shutdown. The `defer db.Close()` on line 66 will never execute in a normal deployment because `http.ListenAndServe` blocks indefinitely — `log.Fatal` (which calls `os.Exit`) bypasses all deferred calls.

## Why It Is an Issue

### 1. `defer db.Close()` never runs

`log.Fatal` internally calls `os.Exit(1)`. [`os.Exit` does not run deferred functions](https://pkg.go.dev/os#Exit). The `defer db.Close()` on line 66 is therefore dead code in every production code path. In normal operation the process runs until killed; when killed, `db.Close()` is skipped.

SQLite's WAL mode is resilient to unclean closes — the database itself won't be corrupted — but the intent to close cleanly is there in the code and it simply doesn't work.

### 2. In-flight HTTP requests are dropped

When Docker sends `SIGTERM` (on `docker stop` or deployment rollout), the process is killed immediately. Any in-progress HTTP request — including `/api/guide` which runs a potentially slow SQLite query over a large dataset — is abruptly terminated, returning a connection reset to the client.

### 3. In-progress XMLTV refresh transactions are rolled back

If the background goroutine is mid-refresh (parsing a 50 MB XML file, or in the middle of writing thousands of airings), an abrupt kill triggers the `defer tx.Rollback()` at the OS level (SQLite will recover via WAL), but the refresh is lost entirely. On the next start the data may be stale.

### 4. Container orchestrators expect graceful shutdown

Docker and Kubernetes send `SIGTERM`, wait for the `StopGracePeriod` (default 10 seconds), then send `SIGKILL`. Applications that handle `SIGTERM` correctly can finish serving in-flight requests within the grace period. Applications that don't are forcibly killed and the graceful-shutdown machinery is wasted.

## What Should Be Done Instead

Use `os/signal` to catch `SIGTERM`/`SIGINT`, then call `http.Server.Shutdown` with a deadline:

```go
srv := &http.Server{
    Addr:    ":" + port,
    Handler: mux,
}

// Start server in background.
go func() {
    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        log.Fatalf("listen: %v", err)
    }
}()

// Block until a shutdown signal is received.
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
<-quit
log.Println("shutting down…")

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := srv.Shutdown(ctx); err != nil {
    log.Printf("forced shutdown: %v", err)
}
db.Close() // safe to call here — server is no longer accepting requests
```

With this pattern:
- `defer db.Close()` can be removed (or replaced by an explicit call after shutdown).
- In-flight requests are drained before the process exits.
- The background ticker goroutine can be stopped via a `context.Context` derived from the same cancellation.

## References

- [`http.Server.Shutdown`](https://pkg.go.dev/net/http#Server.Shutdown) — "Shutdown gracefully shuts down the server without interrupting any active connections."
- [`os/signal` package](https://pkg.go.dev/os/signal)
- [`os.Exit` — deferred functions are not run](https://pkg.go.dev/os#Exit)
- [Go Blog: "Contexts and structs"](https://go.dev/blog/context-and-structs)
- [Kubernetes: Container lifecycle hooks — termination](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination)
