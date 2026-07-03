# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`idp-grc` is a Kubernetes operator written in Go that manages GitHub Actions self-hosted runners inside a cluster.

## Language & Tooling

- **Language**: Go
- **Pattern**: Kubernetes operator (likely using `controller-runtime` and `client-go`)

## Expected Commands

Once the project is initialized with `go mod init`, typical commands will be:

```bash
go build ./...          # Build
go test ./...           # Run all tests
go test ./... -run TestName  # Run a single test
go vet ./...            # Lint/static analysis
```

A `Makefile` is expected for operator scaffold tasks (CRD generation, manifest generation, etc.) following the standard `kubebuilder` or `operator-sdk` conventions.

## Architecture Notes

This project is in early initialization — no Go source files exist yet. When code is added, expect:

- **CRDs**: Custom Resource Definitions defining the runner spec/status
- **Controllers**: Reconciliation loops watching runner CRs and managing pod/deployment lifecycle
- **Webhook**: Admission webhooks for validation/mutation (optional)
- **RBAC**: ClusterRole/Role manifests for the operator's service account

The standard kubebuilder layout is:
- `cmd/` — main entrypoint
- `api/` — CRD type definitions (deepcopy-generated)
- `internal/controller/` — reconciler implementations
- `config/` — Kustomize manifests (CRDs, RBAC, manager deployment)

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan at:
`specs/001-github-runner-operator/plan.md`
<!-- SPECKIT END -->
