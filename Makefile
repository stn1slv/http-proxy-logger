APP_NAME := http-proxy-logger

.PHONY: setup test lint format build run

setup:
	go mod download
	@echo "Install golangci-lint: https://golangci-lint.run/welcome/install/"
	@echo "Install gofumpt:       go install mvdan.cc/gofumpt@latest"

test:
	go test -race -v ./...

lint:
	golangci-lint run ./...

format:
	gofumpt -extra -w .

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(APP_NAME)

run: build
	./$(APP_NAME)
