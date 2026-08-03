VERSION ?= 0.2.0
LDFLAGS  = -s -w -X main.version=$(VERSION)
GOFLAGS  = -trimpath

.PHONY: build ui test vet lint release clean

# Build the prism binary with whatever ui/dist currently holds.
build:
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/prism ./cmd/prism

# Rebuild the embedded dashboard, then the binary.
ui:
	cd ui && npm ci && npm run build
	$(MAKE) build

test:
	go test -race ./...

vet:
	gofmt -l . && go vet ./...

# Cross-compiled release binaries into dist/.
release:
	GOOS=linux  GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/prism-$(VERSION)-linux-amd64 ./cmd/prism
	GOOS=linux  GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/prism-$(VERSION)-linux-arm64 ./cmd/prism
	GOOS=darwin GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/prism-$(VERSION)-darwin-arm64 ./cmd/prism
	GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/prism-$(VERSION)-windows-amd64.exe ./cmd/prism

clean:
	rm -rf bin dist
