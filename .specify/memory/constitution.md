<!--
SYNC IMPACT REPORT
==================
Version change: (unversioned template) → 1.0.0
New constitution — first ratification.

Modified principles: N/A (first version)

Added sections:
  - Core Principles (I–V)
  - Kubernetes-Native Design
  - Production Readiness Standards
  - Governance

Removed sections: N/A

Templates requiring updates:
  - .specify/templates/plan-template.md ✅ — Constitution Check gates align with Go/kubebuilder layout
  - .specify/templates/spec-template.md ✅ — No structural changes required; requirements format compatible
  - .specify/templates/tasks-template.md ✅ — Task phases and parallel markers compatible with operator dev workflow
  - .specify/templates/constitution-template.md ✅ — Source template; no changes needed

Follow-up TODOs:
  - None. All fields resolved from user input and project context.
-->

# idp-grc Constitution

## Core Principles

### I. Kubernetes-Native UX (NON-NEGOTIABLE)

Every feature MUST surface through standard Kubernetes primitives: users define runners as
YAML manifests (CRDs), apply them with `kubectl`, and observe status via `.status` sub-resources.
The operator MUST reconcile declarative state — no imperative runner management scripts or
out-of-band tooling required for normal operation.

**Rationale**: The project's explicit goal is that it "feel like a normal Kubernetes experience."
Anything that forces the user outside of `kubectl apply` / `kubectl get` breaks this contract.

### II. Controller Correctness First

The reconciliation loop MUST be idempotent and level-triggered. Every reconcile call MUST
be safe to run multiple times with the same inputs and produce the same result.
Race conditions, partial updates, and orphaned resources MUST be treated as bugs, not
edge cases. Controller logic MUST handle GitHub API failures gracefully with exponential
backoff — never crash-loop on transient errors.

**Rationale**: Operators run continuously in production clusters. Incorrect reconciliation
leads to resource leaks, duplicate runners, or runners that are permanently stuck.

### III. GitHub Runner Lifecycle Ownership

The operator MUST own the full runner lifecycle end-to-end:
registration token acquisition, runner Pod creation and configuration, runner de-registration
on deletion, and status reporting back to the CRD. The GitHub API MUST be accessed via
the standard documented REST endpoints (runner registration tokens, runner removal).
PAT or GitHub App credentials MUST be consumed from Kubernetes Secrets — never hardcoded
or embedded in the CRD spec.

**Rationale**: Manual steps to register/deregister runners defeat the purpose of the operator.
Secrets management follows Kubernetes security best practices.

### IV. Production-Minded Build and Deployment

The operator binary MUST be buildable into a minimal container image (distroless or scratch base
preferred). Installation MUST be achievable via a single `kubectl apply -f` of a Kustomize bundle
that includes CRDs, RBAC, and the manager Deployment. Leader election MUST be enabled for
high-availability deployments. The operator MUST expose Prometheus metrics and structured logs
(JSON in production, human-readable in development).

**Rationale**: "Production minded" was explicitly requested. Operators without clean install paths
and observability are unusable in real clusters.

### V. Simplicity Over Abstraction

Features MUST NOT be added ahead of a concrete user need. The operator starts with a single
`GitHubRunner` CRD; runner scaling, pools, and webhook triggers are future scope and MUST NOT
be pre-implemented. Complexity MUST be justified explicitly in the plan's Complexity Tracking
table. Three similar reconcile paths are better than a premature abstraction layer.

**Rationale**: Premature abstractions in operators create surface area for bugs and make the
codebase harder to reason about for newcomers.

## Kubernetes-Native Design

### Standard Project Layout

Source MUST follow the kubebuilder scaffolding layout:

- `cmd/` — manager entrypoint (`main.go`)
- `api/v1alpha1/` — CRD type definitions with deepcopy-generated code
- `internal/controller/` — reconciler implementations
- `config/` — Kustomize manifests (CRDs, RBAC, manager Deployment)

Deviations from this layout MUST be documented in the plan with a justification.

### CRD Design Rules

- CRD spec fields MUST use Go struct tags with `+kubebuilder:` markers for validation.
- Status conditions MUST follow the standard `metav1.Condition` pattern (type/status/reason/message).
- All CRD fields MUST have `// +kubebuilder:validation:...` markers or explicit optionality comments.
- API group: `idp.grc.io` | Initial version: `v1alpha1`.

### RBAC Scope

The operator's ServiceAccount MUST follow least-privilege: only the verbs and resources
it actually uses. RBAC manifests MUST be generated via `+kubebuilder:rbac:` markers, not
hand-authored.

## Production Readiness Standards

### Observability

- Structured JSON logging MUST be used in all controller code via `slog` or `zap`.
- Reconcile error rates and durations MUST be exposed as Prometheus metrics (standard
  controller-runtime metrics are acceptable; custom metrics for GitHub API calls are SHOULD).
- Health/readiness probes MUST be configured on the manager Deployment.

### Testing

- Unit tests MUST cover reconciler business logic using `envtest` or controller-runtime's
  fake client — no live cluster required for unit tests.
- Integration tests using `envtest` (real API server, no nodes) SHOULD be present for
  the happy path: create runner CR → runner Pod created, status updated.
- Tests are run with `go test ./...`.

### Documentation

- A `README.md` MUST document: what the operator does, prerequisites, installation steps,
  and a minimal "Getting Started" example (create a `GitHubRunner` CR and verify it).
- The Getting Started section MUST be self-contained enough that a user can try it without
  reading source code.

## Governance

This constitution supersedes all other development practices for `idp-grc`. Amendments MUST:

1. Increment the version number per semantic versioning rules defined below.
2. Update `LAST_AMENDED_DATE` to the date of the amendment.
3. Propagate changes to affected templates and documentation.

**Versioning policy**:
- MAJOR: Backward-incompatible governance change, principle removal, or CRD API group/version change.
- MINOR: New principle or mandatory section added.
- PATCH: Clarification, wording fix, or non-semantic refinement.

**Compliance**: All PRs MUST pass the Constitution Check gate in the plan template before
implementation begins. Complexity violations MUST be documented in the Complexity Tracking table.
The `/speckit-plan` command Constitution Check section lists the active gates derived from this file.

**Version**: 1.0.0 | **Ratified**: 2026-06-30 | **Last Amended**: 2026-06-30
