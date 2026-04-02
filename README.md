# harness-parser

A small Go utility and package for rendering Harness-style Go templates using a YAML values file.

It supports:
- Rendering a single template file
- Rendering a directory of Kubernetes manifest templates
- Helpful template error output with file and line context

## Requirements

- Go 1.26+

## Quick Start

Build the CLI:

```bash
make build
```

Run with the included examples:

```bash
./bin/harness-parser example-template.yaml example-values.yaml
```

## Install CLI

Install into your `GOPATH/bin`:

```bash
make install
```

Then run from anywhere (if `GOPATH/bin` is on your `PATH`):

```bash
harness-parser example-template.yaml example-values.yaml
```

## CLI Usage

```text
harness-parser <template-or-directory> [values-file]
```

- `<template-or-directory>`: required path to a template file or directory
- `[values-file]`: optional values YAML file
- Default values file: `example-values.yaml`

### Single File Example

```bash
harness-parser ./templates/deployment.yaml ./test/values.yaml
```

### Directory Example

```bash
harness-parser ./templates ./test/values.yaml
```

Directory mode behavior:
- Top-level files only (non-recursive)
- Only `.yaml` and `.yml` files are considered
- File must contain both `apiVersion:` and `kind:` to be treated as a Kubernetes manifest
- Matching files are rendered in sorted filename order
- Processing continues when one file fails; failures are summarized at the end

## Strict Mode Behavior

Strict mode is enabled automatically when you pass a custom values file.

Strict mode checks values for unresolved Harness expressions like `<+...>` and fails if any are found.

## Make Targets

```bash
make help
```

Common targets:
- `make build` - build CLI into `bin/harness-parser`
- `make install` - copy CLI into `GOPATH/bin`
- `make run TEMPLATE=<path> VALUES=<file>` - run parser with make variables
- `make deps` - tidy dependencies
- `make clean` - remove build output

## Using as a Go Package

```go
package main

import (
  "fmt"

  harnessparser "harness-parser"
)

func main() {
  out, err := harnessparser.RenderFile("./templates/deployment.yaml", "./values.yaml")
  if err != nil {
    panic(err)
  }

  fmt.Println(out)
}
```

## Example Files

- `example-template.yaml`
- `example-values.yaml`

Use these as a starting point for your own templates and values files.
