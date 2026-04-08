# harness-parser

A small Go utility and package for rendering Harness-style Go templates using one or more YAML values files.

It supports:
- Rendering a single template file
- Rendering a directory of Kubernetes manifest templates
- Layering multiple values files (later files override earlier files)
- Optional interpolation of Harness placeholders for local testing
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
harness-parser [flags] <template-or-directory> [values-file ...]
```

- `<template-or-directory>`: required path to a template file or directory
- `[values-file ...]`: optional one or more values YAML files, or a single values directory
- Default values file: `example-values.yaml`

Flags:
- `-env <name>`: in values directory mode, select only values for the given env
- `-interpolate-secrets`: resolve Harness placeholders from values and fake secrets
- `-secrets-file <path>`: explicitly provide a fake secrets YAML file

When multiple values files are provided, they are merged in argument order.
Later files override earlier files.

If a single values directory is provided, values files are auto-discovered (top-level only):
- `values.yaml` first
- then `values-*.yaml` / `values-*.yml` in lexical order
- `secrets.yaml` and `*.secrets.yaml` are excluded from value layering

With `-env <name>` and values directory mode, selected files are:
- `values.yaml` (always required)
- `values-<env>.yaml` (if present)
- `values-<env>-*.yaml` / `values-<env>-*.yml` in lexical order

Example:

```bash
harness-parser ./templates ./values -env prod
```

### Single File Example

```bash
harness-parser ./templates/deployment.yaml ./test/values.yaml
```

### Directory Example

```bash
harness-parser ./templates ./test/values.yaml
```

### Templates Directory + Values Directory

```bash
harness-parser ./templates ./values
```

### Layered Values Example

```bash
harness-parser ./templates/deployment.yaml ./values/base.yaml ./values/stage.yaml ./values/az1.yaml
```

### Local Interpolation Example

```bash
harness-parser -interpolate-secrets ./templates ./values/values.yaml ./values/values-stage.yaml ./values/values-stage-az1.yaml
```

With `-interpolate-secrets`, if `-secrets-file` is not provided, the parser auto-discovers adjacent files:
- `<values-basename>.secrets.yaml`
- `secrets.yaml`

Example fake secrets file:

```yaml
team/path/db-password: fake-db-password
db-password: fake-db-password
pipeline.variables.tag: test-tag
service.name: encryption-service-test
```

Directory mode behavior:
- Top-level files only (non-recursive)
- Only `.yaml` and `.yml` files are considered
- File must contain both `apiVersion:` and `kind:` to be treated as a Kubernetes manifest
- Matching files are rendered in sorted filename order
- Processing continues when one file fails; failures are summarized at the end

## Strict Mode Behavior

Strict mode is enabled automatically when you pass custom values files.

Strict mode checks values for unresolved Harness expressions like `<+...>` and fails if any are found.
If `-interpolate-secrets` is enabled, interpolation runs before strict validation.

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

Or with layered values:

```go
package main

import (
  "fmt"

  harnessparser "harness-parser"
)

func main() {
  out, err := harnessparser.RenderFileMulti(
    "./templates/deployment.yaml",
    []string{"./values/base.yaml", "./values/stage.yaml", "./values/az1.yaml"},
  )
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
