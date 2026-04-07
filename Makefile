.PHONY: build test test-db test-xmltv test-api test-integration test-ui clean

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

## test-ui: run Playwright UI tests
test-ui:
	npx playwright test --config=e2e/playwright.config.ts --pass-with-no-tests

## dev: run the development environment (tvguide + WireMock)
dev:
	docker compose -f docker-compose.yml -f docker-compose.dev.yml up --build

## clean: remove build output
clean:
	rm -f $(BINARY)

## help: print this help
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
