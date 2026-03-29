.PHONY: build test test-db test-xmltv test-api test-integration clean

BINARY  := tvguide
TIMEOUT := 120s

## build: compile the binary
build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) .

## test: run all tests
test:
	go test -timeout $(TIMEOUT) ./...

## test-db: run database layer tests
test-db:
	go test -timeout $(TIMEOUT) ./internal/database/...

## test-xmltv: run XMLTV parser and network tests (requires Docker for Wiremock)
test-xmltv:
	go test -timeout $(TIMEOUT) ./internal/xmltv/...

## test-api: run API integration tests
test-api:
	go test -timeout $(TIMEOUT) ./internal/api/...

## test-integration: run full integration tests (requires Docker for Wiremock)
test-integration:
	go test -timeout $(TIMEOUT) .

## clean: remove build output
clean:
	rm -f $(BINARY)

## help: print this help
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
