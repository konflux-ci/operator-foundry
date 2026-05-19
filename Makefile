BINARY := operator-foundry
GO := go

.PHONY: build test lint clean

build:
	$(GO) build -o bin/$(BINARY) ./cmd/operator-foundry

test:
	$(GO) test ./... -v -race -count=1

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/
