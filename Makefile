.PHONY: build test lint run docker clean

# Binary name
BINARY := onchain-monitor

## Build the binary
build:
	go build -o $(BINARY) ./cmd/server

## Run locally
run:
	go run ./cmd/server

## Run all tests with race detector
test:
	go test -race -count=1 ./...

## Run tests with coverage report
cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	@echo "---"
	@echo "Open HTML report: go tool cover -html=coverage.out"

## Run linter
lint:
	golangci-lint run --timeout=5m

## Build Docker image
docker:
	docker build -t $(BINARY) .

## Tidy dependencies
tidy:
	go mod tidy

## Clean build artifacts
clean:
	rm -f $(BINARY) coverage.out
