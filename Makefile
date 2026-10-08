GOLANGCI_LINT ?= golangci-lint

.PHONY: fmt lint test coverage check
fmt:
	gofmt -w .
lint:
	@test -z "$$(gofmt -l .)" || (gofmt -l .; exit 1)
	$(GOLANGCI_LINT) run ./...
test:
	go test -race -count=1 ./...
coverage:
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
check: lint test
