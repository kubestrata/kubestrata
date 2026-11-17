.PHONY: build test vet fmt lint check tidy smoke lab-test

BIN := bin/kubestrata

build:
	go build -trimpath -o $(BIN) ./cmd/kubestrata

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

# Fails when a file is not gofmt-formatted; `make fmt-fix` rewrites them.
fmt:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

fmt-fix:
	gofmt -w .

lint: fmt vet
	@if [ -x "$$HOME/go/bin/staticcheck" ]; then $$HOME/go/bin/staticcheck ./...; else echo "staticcheck not installed, skipped"; fi

check: lint test build

tidy:
	go mod tidy

# Smoke test of the built binary against the local kubeconfig (no cluster access).
smoke: build
	$(BIN) --version
	$(BIN) contexts
	$(BIN) contexts --format json

# End-to-end against a running kuberito lab (https://github.com/leosavio/kuberito): collects both
# clusters and compares with the lab's ground truth. Skips when no lab is up.
lab-test:
	go test -tags lab -count=1 -v ./test/e2e/
