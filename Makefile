STATICCHECK ?= $(shell go env GOPATH)/bin/staticcheck

.PHONY: check build

# Всё, что должно быть зелёным перед коммитом.
check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "не отформатировано:"; echo "$$out"; exit 1; fi
	go vet ./...
	$(STATICCHECK) ./...
	go test -race ./...

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/worker ./cmd/worker
