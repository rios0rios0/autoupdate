SCRIPTS_DIR ?= $(HOME)/Development/github.com/rios0rios0/pipelines
-include $(SCRIPTS_DIR)/makefiles/common.mk
-include $(SCRIPTS_DIR)/makefiles/golang.mk

.PHONY: build build-musl debug install run

build:
	rm -rf bin
	go build -o bin/autoupdate ./cmd/autoupdate
	strip -s bin/autoupdate

debug:
	rm -rf bin
	go build -gcflags "-N -l" -o bin/autoupdate ./cmd/autoupdate

build-musl:
	CGO_ENABLED=1 CC=musl-gcc go build \
		--ldflags 'linkmode external -extldflags="-static"' -o bin/autoupdate ./cmd/autoupdate
	strip -s bin/autoupdate

run:
	go run ./cmd/autoupdate

install:
	make build
	mkdir -p ~/.local/bin
	cp -v bin/autoupdate ~/.local/bin/autoupdate
