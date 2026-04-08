.PHONY: all test clean deps build run install

TEMPLATE ?= example-template.yaml
VALUES   ?= example-values.yaml

# Default target
all: test

# Install dependencies
deps:
	go mod tidy

# Build the CLI
build:
	go build -o bin/harness-parser ./cmd

# Install the CLI to $GOBIN (or $GOPATH/bin)
install: build
	cp bin/harness-parser $(shell go env GOPATH)/bin/harness-parser

# Clean build artifacts
clean:
	rm -f bin/harness-parser

# Run the parser with a template and one or more optional values files
# Usage: make run TEMPLATE=./templates VALUES="base.yaml stage.yaml"
run: build
	@if [ -z "$(TEMPLATE)" ]; then \
		echo "Error: TEMPLATE is required. Usage: make run TEMPLATE=<file> [VALUES=\"file1 file2 ...\"]"; \
		exit 1; \
	fi
	./bin/harness-parser $(TEMPLATE) $(VALUES)

help:
	@echo "harness-parser - Render Harness pipeline templates with a values file"
	@echo ""
	@echo "Usage:"
	@echo "  make build                        Build the CLI binary to bin/harness-parser"
	@echo "  make install                      Copy bin/harness-parser to \$$GOPATH/bin"
	@echo "  make deps                         Install/tidy Go dependencies"
	@echo "  make clean                        Remove build artifacts"
	@echo "  make run TEMPLATE=<path>          Parse a template file or directory using example-values.yaml"
	@echo "  make run TEMPLATE=<path> VALUES=\"<file1> [file2 ...]\"  Parse with one or more values files"
	@echo ""
	@echo "Arguments for 'make run':"
	@echo "  TEMPLATE   (required) Path to template file or directory (default: example-template.yaml)"
	@echo "  VALUES     (optional) One or more values YAML files in order (default: example-values.yaml)"
	@echo ""
	@echo "Directory mode rules:"
	@echo "  Top-level only (non-recursive)"
	@echo "  Only .yaml/.yml files with both apiVersion and kind are rendered"
	@echo "  Values files are layered left-to-right (later files override earlier files)"
	@echo ""
	@echo "Example files (start here):"
	@echo "  example-template.yaml             Sample Harness pipeline template"
	@echo "  example-values.yaml               Sample values file matching the example template"
	@echo ""
	@echo "Direct binary usage:"
	@echo "  bin/harness-parser <template-or-directory> [values-file]"
	@echo ""
	@echo "Examples:"
	@echo "  make run                                                   # renders example-template.yaml"
	@echo "  make run TEMPLATE=./templates VALUES=values.yaml           # renders matching k8s files in folder"
	@echo "  make run TEMPLATE=pipeline.yaml VALUES=\"base.yaml stage.yaml az1.yaml\""
	@echo "  make run TEMPLATE=pipeline.yaml"
	@echo "  make run TEMPLATE=pipeline.yaml VALUES=prod-values.yaml"
	@echo "  bin/harness-parser pipeline.yaml base.yaml stage.yaml az1.yaml"
	@echo "  bin/harness-parser pipeline.yaml                           # uses example-values.yaml"
	@echo "  bin/harness-parser ./templates values.yaml                 # directory mode"
	@echo "  bin/harness-parser pipeline.yaml my-values.yaml            # strict mode"
