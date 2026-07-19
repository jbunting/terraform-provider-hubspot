default: build

.PHONY: build install lint generate fmt test testacc testacc-real sweep

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

# Run acceptance tests against a real HubSpot portal. Requires TF_ACC=1 (set
# below), HUBSPOT_ACCESS_TOKEN (dedicated test portal private-app token) and
# HUBSPOT_TEST_PORTAL_ID; see internal/provider/real_api_test.go for the
# env contract and portal safety guard.
testacc-real:
	TF_ACC=1 go test ./internal/provider/ -run 'TestAccReal' -v -count=1 -timeout 30m

sweep: ## Delete leaked tf_acc_test_* resources from the real test portal
	go test ./internal/provider/ -v -sweep=all -timeout 10m
