.DEFAULT_GOAL := build

.PHONY: build install test lint clean

build:
	go build -o syno .

install:
	go install .

test:
	go vet ./...
	go test ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf syno dist
