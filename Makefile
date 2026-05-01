SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c

BIN ?= terraform-provider-pxgrid
OS ?= $(shell go env GOOS)
ARCH ?= $(shell go env GOARCH)
OUT_DIR ?= dist/$(OS)_$(ARCH)

.PHONY: build test clean

build:
	mkdir -p "$(OUT_DIR)"
	GOOS="$(OS)" GOARCH="$(ARCH)" go build -o "$(OUT_DIR)/$(BIN)" .

test:
	go test ./...

clean:
	rm -rf dist
