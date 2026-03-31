# Review: Package-Level HTTP Client Prevents Context Propagation and Request Cancellation

## Problem

[internal/xmltv/parser.go lines 120–122](../internal/xmltv/parser.go#L120) declares a package-level `http.Client`:

```go
var httpClient = &http.Client{
    Timeout: 5 * time.Minute,
}
```

The `Fetch` function uses this client directly and does not accept a `context.Context`:

```go
func Fetch(url string) (*TV, error) {
    req, err := http.NewRequest(http.MethodGet, url, nil)
    ...
    resp, err := httpClient.Do(req)
    ...
}
```

## Why It Is an Issue

### 1. No context propagation means no cancellation

In Go, the idiomatic way to propagate deadlines, timeouts, and cancellation across API boundaries is via `context.Context`. Without it:

- When a graceful shutdown is initiated (see review 05), an in-progress 50 MB XMLTV download cannot be cancelled. The shutdown must either wait the full 5 minutes or kill the process.
- If the caller sets a tighter deadline (e.g., during a test), that deadline cannot flow into the HTTP request.
- Future callers (e.g., an admin endpoint that triggers a manual refresh) have no way to respect their request's context.

### 2. Package-level mutable state makes tests fragile

The `httpClient` variable is package-level and exported by side-effect (tests can replace it). Package-level state makes tests order-dependent if any test modifies the variable, and it makes the dependency implicit rather than explicit. A function that needs an HTTP client should receive one.

### 3. The `http.NewRequest` + `httpClient.Do` split is already half-way to the context-aware pattern

`http.NewRequestWithContext` (available since Go 1.13) is the preferred form when a context is available. Using the older `http.NewRequest` pattern alongside a package-level client without context is an outdated pattern.

### 4. The 5-minute timeout is not configurable

A package-level client bakes in the timeout. If a particularly large XMLTV feed requires a longer timeout, or if tests need a shorter one, there is no way to adjust it without either modifying the source or introducing a global variable mutation in tests.

## What Should Be Done Instead

Pass the context and the HTTP client through the function signature:

```go
// Fetch downloads and parses an XMLTV document from the given URL.
// The provided context controls the lifetime of the HTTP request.
func Fetch(ctx context.Context, client *http.Client, url string) (*TV, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return nil, fmt.Errorf("building request: %w", err)
    }
    req.Header.Set("Accept", "text/xml, application/xml, */*")
    req.Header.Set("User-Agent", "xmltvguide/1.0")

    resp, err := client.Do(req)
    ...
}
```

The caller in `main.go` creates the client once and passes it down:

```go
httpClient := &http.Client{Timeout: 5 * time.Minute}
// ... pass to refresh() and ultimately to xmltv.Fetch()
```

Tests can pass `httptest.NewServer`'s URL and `httptest.NewServer`'s default client (or any `*http.Client` with a short timeout) without touching any global state.

If the full signature change is considered too disruptive at this stage, a minimum improvement is to replace `http.NewRequest` with `http.NewRequestWithContext` and thread a context from the call site, keeping the package-level client temporarily:

```go
func Fetch(ctx context.Context, url string) (*TV, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    ...
}
```

## References

- [`http.NewRequestWithContext`](https://pkg.go.dev/net/http#NewRequestWithContext) — introduced in Go 1.13
- [`context` package](https://pkg.go.dev/context)
- [Go Blog: "Go Concurrency Patterns: Context"](https://go.dev/blog/context)
- [Go Wiki: "CodeReviewComments — Contexts"](https://github.com/golang/go/wiki/CodeReviewComments#contexts) — "Do not store Contexts inside a struct type; instead, pass a Context explicitly to each function that needs it."
