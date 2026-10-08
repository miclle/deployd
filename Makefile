GOLANGCI_LINT ?= golangci-lint
# Keep lint on the Go minor version used by CI and supported by golangci-lint.
LINT_GOTOOLCHAIN ?= go1.26.6

.PHONY: fmt lint test coverage check
fmt:
	gofmt -w .
lint:
	@test -z "$$(gofmt -l .)" || (gofmt -l .; exit 1)
	GOTOOLCHAIN=$(LINT_GOTOOLCHAIN) $(GOLANGCI_LINT) run ./...
test:
	go test -race -count=1 ./...
coverage:
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	awk -f scripts/check-coverage.awk coverage.out
check: lint test
