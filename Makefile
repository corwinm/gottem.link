.PHONY: backup-test build check container-test fmt fmt-check race run test tidy-check tools vet vuln

GO ?= go
GOVULNCHECK ?= ./.bin/govulncheck
GO_FILES := $(shell git ls-files '*.go')

fmt:
	gofmt -w $(GO_FILES)

fmt-check:
	@files="$$(gofmt -l $(GO_FILES))"; \
	if [ -n "$$files" ]; then printf 'Run make fmt:\n%s\n' "$$files"; exit 1; fi

tidy-check:
	$(GO) mod tidy -diff

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

vuln:
	@test -x $(GOVULNCHECK) || { echo 'Run make tools first'; exit 1; }
	$(GOVULNCHECK) ./...

build:
	$(GO) build ./...

backup-test:
	scripts/test-backup-restore

container-test:
	scripts/test-container

check: fmt-check tidy-check test race vet vuln build backup-test

tools:
	mkdir -p .bin
	GOBIN=$(CURDIR)/.bin GOTOOLCHAIN=$$($(GO) env GOVERSION) $(GO) install golang.org/x/vuln/cmd/govulncheck@v1.7.0

run:
	$(GO) run . -addr :8080 -dsn ./gottem.db
