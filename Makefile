VERSION=1.2.0
BINARY=codeforge
LDFLAGS=-ldflags="-s -w -X codeforge/cmd.Version=$(VERSION)"

build:
	go build $(LDFLAGS) -o $(BINARY) .

build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY)-linux .

build-mac-arm:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY)-mac-arm .

build-mac-intel:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY)-mac-intel .

build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY).exe .

build-headless:
	CGO_ENABLED=0 go build -tags headless $(LDFLAGS) -o $(BINARY)-headless .

build-linux-headless:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags headless $(LDFLAGS) -o $(BINARY)-headless-linux .

build-all: build-linux build-mac-arm build-mac-intel build-windows build-headless build-linux-headless

test:
	go test ./... -v

install:
	go install $(LDFLAGS) .

clean:
	rm -f $(BINARY) $(BINARY)-linux $(BINARY)-mac-arm $(BINARY)-mac-intel $(BINARY).exe $(BINARY)-headless $(BINARY)-headless-linux

.PHONY: build build-linux build-mac-arm build-mac-intel build-windows build-headless build-linux-headless build-all test install clean
