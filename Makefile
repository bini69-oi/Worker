STATICCHECK ?= $(shell go env GOPATH)/bin/staticcheck
GOVULNCHECK ?= $(shell go env GOPATH)/bin/govulncheck

.PHONY: check vuln build types web

# Всё, что должно быть зелёным перед коммитом.
check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "не отформатировано:"; echo "$$out"; exit 1; fi
	go vet ./...
	$(STATICCHECK) ./...
	go test -race ./...

# Известные уязвимости в Go и зависимостях, до которых доходит наш код. Нужна сеть.
vuln:
	$(GOVULNCHECK) ./...

build: web
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/worker ./cmd/worker

# Типы фронта из структур ответов api. CI запускает это и падает, если файл изменился.
types:
	go run ./cmd/gen-types -o web/src/lib/types.gen.ts

# Сборка фронта в web/build, оттуда её вшивает бинарник. Нужен Node.js, пользователю он не нужен.
web:
	cd web && npm ci && npm run build
