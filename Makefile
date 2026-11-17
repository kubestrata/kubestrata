.PHONY: build test vet lint check tidy smoke

BIN := bin/kubestrata

build:
	go build -trimpath -o $(BIN) ./cmd/kubestrata

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

lint: vet
	@if [ -x "$$HOME/go/bin/staticcheck" ]; then $$HOME/go/bin/staticcheck ./...; else echo "staticcheck not installed, skipped"; fi

check: lint test build

tidy:
	go mod tidy

# Smoke test of the built binary against the local kubeconfig (no cluster access).
smoke: build
	$(BIN) --version
	$(BIN) contexts
	$(BIN) contexts --format json
