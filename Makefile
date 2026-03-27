BUILD_DIR := ./build/bin
GOBIN := $(shell go env GOPATH)/bin

GOLANGCI_LINT_VERSION := v2.11.4
GOLANGCI_LINT := $(shell which golangci-lint 2>/dev/null || echo $(GOBIN)/golangci-lint)

.PHONY: build rebuild rpc_int rpc_perf rpc_archive fmt lint test clean

build: rpc_int rpc_perf

rebuild: clean build

rpc_int: $(BUILD_DIR)/rpc_int

rpc_perf: $(BUILD_DIR)/rpc_perf

rpc_archive: $(BUILD_DIR)/rpc_archive

$(BUILD_DIR)/rpc_int: $(shell find cmd/integration internal -name '*.go')
	go build -o $@ ./cmd/integration/

$(BUILD_DIR)/rpc_perf: $(shell find cmd/perf internal -name '*.go')
	go build -o $@ ./cmd/perf/

$(BUILD_DIR)/rpc_archive: $(shell find cmd/archive -name '*.go')
	go build -o $@ ./cmd/archive/

fmt:
	gofmt -w .

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run

$(GOBIN)/golangci-lint:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

test:
	go test ./...

clean:
	rm -rf $(BUILD_DIR)
