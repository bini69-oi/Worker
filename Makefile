STATICCHECK ?= $(shell go env GOPATH)/bin/staticcheck
GOVULNCHECK ?= $(shell go env GOPATH)/bin/govulncheck

.PHONY: check vuln build

# Всё, что должно быть зелёным перед коммитом.
check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "не отформатировано:"; echo "$$out"; exit 1; fi
	go vet ./...
	$(STATICCHECK) ./...
	go test -race ./...

# Известные уязвимости в Go и зависимостях, до которых доходит наш код. Нужна сеть.
vuln:
	$(GOVULNCHECK) ./...

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/worker ./cmd/worker
