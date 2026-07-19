default: build

.PHONY: build install lint generate fmt test testacc sweep

build:
	go build ./...

install:
	go install

lint:
	golangci-lint run

# Generate documentation via tfplugindocs (see tools/tools.go).
generate:
	cd tools; go generate ./...

fmt:
	gofmt -s -w -e .

test:
	go test ./... -count=1 -timeout=5m

# Run acceptance tests.
testacc:
	TF_ACC=1 go test ./... -v -count=1 -timeout 120m

# Sweepers clean up leaked test infrastructure.
# Placeholder: add `go test ./internal/provider/ -v -sweep=all` once sweepers exist.
sweep:
	@echo "No sweepers implemented yet."
