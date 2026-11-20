# stctl — Subtensor network management CLI for the UR subnet.
# Mirrors the build conventions of urnetwork/connect (provider/Makefile).

STCTL_VERSION ?= 0.0.1
GOOS_ARCH ?= darwin/arm64 darwin/amd64 linux/amd64 linux/arm64

.PHONY: build clean vet test

build:
	@for osarch in $(GOOS_ARCH); do \
		os=$${osarch%/*}; arch=$${osarch#*/}; \
		echo "building $$os/$$arch"; \
		env CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -trimpath -ldflags "-w -s -X main.Version=$(STCTL_VERSION)" \
			-o build/$$os/$$arch/stctl . ; \
	done

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -rf build
