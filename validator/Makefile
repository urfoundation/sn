# validator — the UR subnet on-chain validator binary (PLAN.md §7.2).
# Mirrors the build conventions of connect/provider/Makefile (as snclaim and
# stctl already do), adjusted to this binary's name.

VALIDATOR_VERSION ?= 0.0.1
GOOS_ARCH ?= darwin/arm64 darwin/amd64 linux/amd64 linux/arm64

.PHONY: all build clean vet test

all: clean build

build:
	@for osarch in $(GOOS_ARCH); do \
		os=$${osarch%/*}; arch=$${osarch#*/}; \
		echo "building $$os/$$arch"; \
		env CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -trimpath -ldflags "-w -s -X main.Version=$(VALIDATOR_VERSION)" \
			-o build/$$os/$$arch/validator ../cli/validator ; \
	done

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -rf build
