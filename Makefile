BINARY := k8s-mcp

.PHONY: build run test lint tidy clean

build:
	go build -o $(BINARY) .

run: build
	./$(BINARY)

test:
	go test ./...

lint:
	go vet ./...
	golangci-lint run

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)
