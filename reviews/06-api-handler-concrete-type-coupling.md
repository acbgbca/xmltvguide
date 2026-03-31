# Review: API Handler Depends on Concrete Type Instead of an Interface

## Problem

The API [Handler](../internal/api/handlers.go#L12) takes a direct pointer to the concrete `*database.DB` type:

```go
// internal/api/handlers.go
type Handler struct {
    db *database.DB
}

func New(db *database.DB) *Handler {
    return &Handler{db: db}
}
```

The `api` package therefore imports and couples itself to the `database` package.

## Why It Is an Issue

### 1. Tight coupling between packages

The `api` package and the `database` package are now coupled: if `database.DB` changes its method signatures, the `api` package is also broken. In a clean architecture, the HTTP layer should depend on an abstraction, not on a specific storage implementation.

### 2. Harder to test in isolation

The [handler tests](../internal/api/handlers_test.go) work around this by creating a real SQLite database for every test run. That is heavier than necessary — if `Handler` accepted an interface, tests could pass a simple in-memory stub that returns predetermined data, making handler tests fast and free of I/O.

### 3. Violates the Go interface convention

Go's idiomatic approach is to define interfaces at the point of *use*, not at the point of *implementation*. The `api` package should declare the narrow interface it needs:

```go
// Defined in api/handlers.go — only what the handler actually calls.
type store interface {
    GetChannels() ([]database.Channel, error)
    GetAirings(date time.Time) ([]database.Airing, error)
    GetStatus() database.Status
}
```

`*database.DB` satisfies this interface implicitly, so no changes to the database package are required. Any test double that implements the three methods also satisfies it.

### 4. Circular import risk grows over time

Right now `api` imports `database`. If `database` ever needed to reference anything in `api` (unlikely but not impossible as a project grows), a circular import would result and the compiler would reject it. An interface boundary makes this structurally impossible.

## What Should Be Done Instead

Define an interface in the `api` package describing only the methods the handlers call:

```go
// internal/api/handlers.go

type store interface {
    GetChannels() ([]database.Channel, error)
    GetAirings(date time.Time) ([]database.Airing, error)
    GetStatus() database.Status
}

type Handler struct {
    db store
}

func New(db store) *Handler {
    return &Handler{db: db}
}
```

The `database` return types (`Channel`, `Airing`, `Status`) can still be imported from `database` — that is appropriate because those are data types, not behaviour. The coupling being removed is the dependency on `*database.DB` as the concrete implementation.

Handler tests can then use a simple struct that implements `store` without creating a real database.

## References

- [Effective Go — interfaces](https://go.dev/doc/effective_go#interfaces)
- [Go Blog: "The Go Programming Language Specification — Interface types"](https://go.dev/ref/spec#Interface_types)
- [Go Wiki: "CodeReviewComments — Interfaces"](https://github.com/golang/go/wiki/CodeReviewComments#interfaces) — "Go interfaces generally belong in the package that *uses* values of the interface type, not the package that *implements* those values."
- [Rob Pike: "Go Proverbs"](https://go-proverbs.github.io/) — "The bigger the interface, the weaker the abstraction."
