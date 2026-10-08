.DEFAULT_GOAL := check

export GOTOOLCHAIN ?= local
GOLANGCI_LINT ?= golangci-lint

# Keep lint on the Go minor version used by CI and supported by golangci-lint.
LINT_GOTOOLCHAIN ?= go1.26.6

.PHONY: check fmt fmt-check gomod lint test coverage

check: gomod lint coverage

fmt:
	gofmt -w .

fmt-check:
	@set -e; \
	unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		printf 'Run make fmt to format these files:\n%s\n' "$$unformatted"; \
		exit 1; \
	fi

gomod:
	go mod tidy -diff

lint: fmt-check
	$(GOLANGCI_LINT) config verify
	GOTOOLCHAIN=$(LINT_GOTOOLCHAIN) $(GOLANGCI_LINT) run ./...

test:
	go test -race -count=1 ./...

coverage:
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	awk -f scripts/check-coverage.awk coverage.out
