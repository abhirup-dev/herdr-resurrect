SHELL := /bin/bash

GO ?= go
GOFMT ?= gofmt
HERDR ?= herdr
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
BINARY := bin/herdr-archive
INSTALLED_BINARY := $(BINDIR)/herdr-archive

.DEFAULT_GOAL := help

.PHONY: help build format check install install-cli install-plugin install-keybinding clean

help:
	@printf '%s\n' \
	  'make build              Build bin/herdr-archive' \
	  'make format             Format all Go source files' \
	  'make check              Check formatting, compile packages, and run go vet' \
	  'make install            Install CLI, link plugin, and add prefix+R' \
	  'make install-cli        Install the CLI under $(BINDIR)' \
	  'make install-plugin     Build and link this checkout into Herdr' \
	  'make install-keybinding Add prefix+R to the Herdr config once' \
	  'make clean              Remove the local build artifact' \
	  '' \
	  'Overrides: PREFIX=… BINDIR=… GO=… GOFMT=… HERDR=… HERDR_CONFIG_PATH=…'

build:
	@mkdir -p bin
	$(GO) build -o $(BINARY) .

format:
	@find . -name '*.go' -not -path './.serena/*' -print0 | xargs -0 $(GOFMT) -w

check:
	@unformatted="$$(find . -name '*.go' -not -path './.serena/*' -print0 | xargs -0 $(GOFMT) -l)"; \
	  if [[ -n "$$unformatted" ]]; then \
	    printf 'gofmt required:\n%s\n' "$$unformatted"; \
	    exit 1; \
	  fi
	$(GO) test ./...
	$(GO) vet ./...

install: install-cli install-plugin install-keybinding
	@printf 'herdr-archive installed: %s\n' "$(INSTALLED_BINARY)"

install-cli: build
	install -d "$(BINDIR)"
	@if cmp -s "$(BINARY)" "$(INSTALLED_BINARY)" 2>/dev/null; then \
	  printf 'CLI already current: %s\n' "$(INSTALLED_BINARY)"; \
	else \
	  install -m 0755 "$(BINARY)" "$(INSTALLED_BINARY)"; \
	fi

install-plugin: build
	$(HERDR) plugin link "$(CURDIR)"

install-keybinding:
	HERDR_BIN_PATH="$(HERDR)" HERDR_CONFIG_PATH="$(HERDR_CONFIG_PATH)" scripts/install-keybinding.sh

clean:
	rm -f "$(BINARY)"
