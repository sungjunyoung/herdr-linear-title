# Run targets inside the flake dev shell (direnv `use flake`, or
# `nix develop -c make <target>`), which provides go, golangci-lint and make.

BIN := bin/herdr-linear-title
NIX := nix --extra-experimental-features 'nix-command flakes'

.PHONY: build test lint fmt link install-app clean

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
	node --test herdr-app/mods/linear-title/policy.test.mjs

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

# Registers this working tree as a local herdr plugin. `herdr plugin link`
# does not run [[build]], so build first.
link: build
	herdr plugin link "$(CURDIR)"

# Installs the binary and links the mod into the Herdr desktop app, which
# reloads mods by itself.
APP_MOD := $(HOME)/.config/herdr-app/mods/linear-title
install-app: build
	mkdir -p $(HOME)/.local/bin
	install -m 0755 $(BIN) $(HOME)/.local/bin/herdr-linear-title
	ln -sfn "$(CURDIR)/herdr-app/mods/linear-title" "$(APP_MOD)"

clean:
	rm -rf bin result
