SCRIPTS_DIR ?= $(HOME)/Development/github.com/rios0rios0/pipelines
-include $(SCRIPTS_DIR)/makefiles/common.mk
-include $(SCRIPTS_DIR)/makefiles/golang.mk

build:
	rm -rf bin
	go build -o bin/autoupdate .
	strip -s bin/autoupdate

debug:
	rm -rf bin
	go build -gcflags "-N -l" -o bin/autoupdate .

build-musl:
	CGO_ENABLED=1 CC=musl-gcc go build \
		--ldflags 'linkmode external -extldflags="-static"' -o bin/autoupdate .
	strip -s bin/autoupdate

run:
	go run .

install:
	sudo cp -v bin/autoupdate /usr/local/bin/autoupdate
