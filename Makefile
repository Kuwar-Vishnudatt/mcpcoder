.PHONY: build test fmt vet clean

build:
	go build -o bin/mcpcoder ./cmd/mcpcoder

test:
	go test ./... -count=1

test-race:
	go test ./... -race -count=1

fmt:
	gofmt -l .

vet:
	go vet ./...

clean:
	rm -rf bin/
