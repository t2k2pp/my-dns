BINDIR   := ./bin
LDFLAGS  := -s -w

.PHONY: all build dns-server log-analyzer bl-manager clean run install tidy vet

all: build

## build – compile all three binaries into ./bin/
build: dns-server log-analyzer bl-manager

dns-server:
	@mkdir -p $(BINDIR)
	go build $(LDFLAGS:%=-ldflags "%") -o $(BINDIR)/dns-server ./cmd/dns-server

log-analyzer:
	@mkdir -p $(BINDIR)
	go build $(LDFLAGS:%=-ldflags "%") -o $(BINDIR)/log-analyzer ./cmd/log-analyzer

bl-manager:
	@mkdir -p $(BINDIR)
	go build $(LDFLAGS:%=-ldflags "%") -o $(BINDIR)/bl-manager ./cmd/bl-manager

## run – start dns-server (requires config.yaml; uses sudo for port 53)
run: build
	sudo $(BINDIR)/dns-server -config config.yaml

## install – copy binaries to /usr/local/bin (requires sudo)
install: build
	sudo install -m 755 $(BINDIR)/dns-server   /usr/local/bin/dns-server
	sudo install -m 755 $(BINDIR)/log-analyzer /usr/local/bin/log-analyzer
	sudo install -m 755 $(BINDIR)/bl-manager   /usr/local/bin/bl-manager
	@echo "Installed all binaries to /usr/local/bin"

## tidy – update go.sum
tidy:
	go mod tidy

## vet – run go vet on all packages
vet:
	go vet ./...

## clean – remove build artefacts
clean:
	rm -rf $(BINDIR)
