# Run targets inside the flake dev shell (direnv `use flake`, or
# `nix develop -c make <target>`), which provides go, golangci-lint and make.

BIN := bin/herdr-linear-title
NIX := nix --extra-experimental-features 'nix-command flakes'

.PHONY: build test lint fmt link clean

# Builds with nix and copies the binary into ./bin.
#
# The binary is copied out of the nix store on purpose: `herdr plugin install`
# builds in a temporary checkout and then renames it, which would orphan a
# `result` out-link GC root and let `nix-collect-garbage` delete the binary.
build:
	@out=$$($(NIX) build .#default --no-link --print-out-paths) && \
		mkdir -p bin && \
		install -m 0755 "$$out/bin/herdr-linear-title" $(BIN)

test:
	go test -race ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

# Registers this working tree as a local herdr plugin. `herdr plugin link`
# does not run [[build]], so build first.
link: build
	herdr plugin link "$(CURDIR)"

clean:
	rm -rf bin result
