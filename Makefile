VERSION ?= 0.1.0-dev
BINARY  := dscd

.PHONY: build build-linux clean test

build:
	CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) ./cmd/dscd/

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY)-linux ./cmd/dscd/

test:
	go test ./...

clean:
	rm -f $(BINARY) $(BINARY)-linux
