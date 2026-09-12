APP_NAME := http-proxy-logger
# Strip the leading "v" (or "v.") like the release workflow does.
VERSION := $(shell (git describe --tags --always --dirty 2>/dev/null || echo dev) | sed -E 's/^v\.?//')

.PHONY: setup test lint format build run clean upgrade-deps docker-build

setup:
	go mod download
	@echo "Install golangci-lint: https://golangci-lint.run/welcome/install/"
	@echo "Install gofumpt:       go install mvdan.cc/gofumpt@latest"

test:
	go test -race -shuffle=on -v ./...

lint:
	golangci-lint run ./...

format:
	gofumpt -extra -w .

build:
	CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$(VERSION)" -o $(APP_NAME)

run: build
	./$(APP_NAME)

clean:
	rm -f $(APP_NAME) coverage.out

upgrade-deps:
	go get -u ./...
	go mod tidy

docker-build:
	docker build -t stn1slv/http-proxy-logger .
