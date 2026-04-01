SCRIPTS_DIR ?= $(HOME)/Development/github.com/rios0rios0/pipelines
-include $(SCRIPTS_DIR)/makefiles/common.mk
-include $(SCRIPTS_DIR)/makefiles/golang.mk

VERSION ?= $(shell sh -c 'git describe --tags --abbrev=0 2>/dev/null || echo "dev"' | sed 's/^v//')
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build build-musl debug install run

build:
	rm -rf bin
	go build -ldflags "$(LDFLAGS) -s -w" -o bin/autoupdate ./cmd/autoupdate

debug:
	rm -rf bin
	go build -gcflags "-N -l" -ldflags "$(LDFLAGS)" -o bin/autoupdate ./cmd/autoupdate

build-musl:
	CGO_ENABLED=1 CC=musl-gcc go build \
		-ldflags "$(LDFLAGS) -linkmode external -extldflags=-static -s -w" -o bin/autoupdate ./cmd/autoupdate

run:
	go run ./cmd/autoupdate

install:
	make build
	mkdir -p ~/.local/bin
	cp -v bin/autoupdate ~/.local/bin/autoupdate
