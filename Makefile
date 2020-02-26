# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
BINARY_NAME=bouncer

all: test build
build:
	$(GOBUILD) -o bin/$(BINARY_NAME)_server cmd/server/main.go
test:
	$(GOTEST) -v ./...
clean:
	$(GOCLEAN)
	rm -rf bin/
