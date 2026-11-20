# validator — the UR subnet on-chain validator binary (PLAN.md §7.2).
# Mirrors the build conventions of connect/provider/Makefile (as snclaim and
# stctl already do), adjusted to this binary's name.

VALIDATOR_VERSION ?= $(shell git describe --always --dirty)
GOOS_ARCH ?= darwin/arm64 darwin/amd64 linux/amd64 linux/arm64

.PHONY: all build package clean vet test

all: clean build

build:
	@set -eu; for osarch in $(GOOS_ARCH); do \
		os=$${osarch%/*}; arch=$${osarch#*/}; \
		echo "building $$os/$$arch"; \
		env CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -trimpath -ldflags "-w -s -X main.Version=$(VALIDATOR_VERSION)" \
			-o build/$$os/$$arch/validator ../cli/validator ; \
	done

# Each archive is self-contained: runners need no sibling Go checkouts.
package: build
	@set -eu; mkdir -p build/releases; \
	for osarch in $(GOOS_ARCH); do \
		os=$${osarch%/*}; arch=$${osarch#*/}; \
		stage=$$(mktemp -d build/package.XXXXXXXX); \
		trap 'rm -rf "$$stage"' EXIT HUP INT TERM; \
		cp build/$$os/$$arch/validator "$$stage/validator"; \
		cp README.md release.yml mainnet-profile.json "$$stage/"; \
		COPYFILE_DISABLE=1 tar -czf "build/releases/validator-$(VALIDATOR_VERSION)-$$os-$$arch.tar.gz" -C "$$stage" .; \
		rm -rf "$$stage"; \
	done

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -rf build
