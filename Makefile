.PHONY: test vet build clean

test:
	go test ./...

vet:
	go vet ./...

build:
	mkdir -p dist
	go build -trimpath -o dist/ai-gateway ./cmd/ai-gateway

clean:
	rm -rf dist
