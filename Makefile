.PHONY: build test vet
build:
	go build -o hostrunner ./cmd/hostrunner
test:
	go test ./...
vet:
	go vet ./...
