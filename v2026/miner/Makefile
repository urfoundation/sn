
.PHONY: all clean build init test test_windows_arm64 warp_build warp_build_image

all: init clean build

clean:
	rm -rf build

build:
	env GOEXPERIMENT=greenteagc CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-w -s -X main.Version=${WARP_VERSION}" -o build/linux/arm64/miner ../cli/miner
	env GOEXPERIMENT=greenteagc CGO_ENABLED=0 GOOS=linux GOARCH=arm go build -trimpath -ldflags "-w -s -X main.Version=${WARP_VERSION}" -o build/linux/arm/miner ../cli/miner
	env GOEXPERIMENT=greenteagc CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-w -s -X main.Version=${WARP_VERSION}" -o build/linux/amd64/miner ../cli/miner
	env GOEXPERIMENT=greenteagc CGO_ENABLED=0 GOOS=linux GOARCH=386 go build -trimpath -ldflags "-w -s -X main.Version=${WARP_VERSION}" -o build/linux/386/miner ../cli/miner
	env GOEXPERIMENT=greenteagc CGO_ENABLED=0 GOOS=linux GOARCH=mips GOMIPS=softfloat go build -trimpath -ldflags "-w -s -X main.Version=${WARP_VERSION}" -o build/linux/mips/miner ../cli/miner
	env GOEXPERIMENT=greenteagc CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -trimpath -ldflags "-w -s -w -s -X main.Version=${WARP_VERSION}" -o build/linux/mipsle/miner ../cli/miner
	env GOEXPERIMENT=greenteagc CGO_ENABLED=0 GOOS=linux GOARCH=mips64 GOMIPS=softfloat go build -trimpath -ldflags "-w -s -X main.Version=${WARP_VERSION}" -o build/linux/mips64/miner ../cli/miner
	env GOEXPERIMENT=greenteagc CGO_ENABLED=0 GOOS=linux GOARCH=mips64le GOMIPS=softfloat go build -trimpath -ldflags "-w -s -w -s -X main.Version=${WARP_VERSION}" -o build/linux/mips64le/miner ../cli/miner
	env GOEXPERIMENT=greenteagc CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-w -s -X main.Version=${WARP_VERSION}" -o build/darwin/arm64/miner ../cli/miner
	env GOEXPERIMENT=greenteagc CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "-w -s -X main.Version=${WARP_VERSION}" -o build/darwin/amd64/miner ../cli/miner
	env GOEXPERIMENT=greenteagc CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -trimpath -ldflags "-w -s -X main.Version=${WARP_VERSION}" -o build/windows/arm64/miner.exe ../cli/miner
	env GOEXPERIMENT=greenteagc CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-w -s -X main.Version=${WARP_VERSION}" -o build/windows/amd64/miner.exe ../cli/miner
	COPYFILE_DISABLE=1 tar -czf build/miner.tar.gz -C build linux darwin windows

test: test_windows_arm64
	go test ./...

# This cross-compilation is intentional: the substrate RPC client's archived
# npipe dependency compiled on windows/amd64 but failed on windows/arm64.
test_windows_arm64:
	env GOEXPERIMENT=greenteagc CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -trimpath -o /dev/null ../cli/miner

warp_build:
	@echo "warp_build must be run through warpctl build so govulncheck gates image publication." >&2
	@false

warp_build_image:
	docker buildx build --progress=plain \
		--build-arg warp_env=${WARP_ENV} \
		--platform linux/arm64/v8,linux/amd64 \
		-t ${WARP_DOCKER_NAMESPACE}/${WARP_DOCKER_IMAGE}:${WARP_DOCKER_VERSION} \
		--no-cache \
		--push \
		.

init:
	go clean -cache
	go clean -modcache
