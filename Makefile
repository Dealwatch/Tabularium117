# Tabularium 117 build entry points. Windows users can use build.ps1 instead.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build build-host test vet fmt check clean

## build: cross-compile the Windows release binary into dist/
build:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/tabularium117.exe ./cmd/tabularium117

## build-host: build for the current platform (development)
build-host:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/tabularium117 ./cmd/tabularium117

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

## check: everything the repository knows how to verify
check:
	./scripts/check.sh

clean:
	rm -rf dist
