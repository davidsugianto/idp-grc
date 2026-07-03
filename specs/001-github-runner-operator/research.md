# Research: GitHub Actions Self-Hosted Runner Operator

**Feature**: `001-github-runner-operator`
**Date**: 2026-06-30

---

## Decision 1: Go Version

**Decision**: Go 1.22  
**Rationale**: kubebuilder v4.1.1 (current stable) requires a minimum of Go 1.22. Using 1.22 ensures
compatibility with the full kubebuilder v4 scaffolding toolchain and controller-runtime v0.18.x.  
**Alternatives considered**: Go 1.21 — incompatible with kubebuilder v4.1.1 minimum requirements.

---

## Decision 2: Operator Framework — kubebuilder v4 + controller-runtime v0.18

**Decision**: kubebuilder v4.1.1 scaffolding; controller-runtime v0.18.4 as the operator framework.  
**Rationale**: kubebuilder v4 is the current stable release and uses the `go/v4` plugin layout. It ships
controller-runtime v0.18.4, which provides the reconciler interface, envtest utilities, leader election,
metrics server, and health probe server needed by the project constitution's production readiness
requirements. All kubebuilder markers (RBAC, CRD validation, printer columns, status subresource) are
supported out of the box.  
**Alternatives considered**: operator-sdk (wraps kubebuilder, adds overhead not needed here);
bare client-go without controller-runtime (loses reconciliation loop infrastructure, leader election, envtest).

---

## Decision 3: GitHub API — Registration Token Strategy

**Decision**: Use GitHub REST API registration token endpoints. Tokens are fetched on every reconcile
that requires registration (not cached beyond a single reconcile attempt), since they expire in 1 hour.

**Repository runner registration token**:
```
POST /repos/{owner}/{repo}/actions/runners/registration-token
Authorization: token {PAT}
Accept: application/vnd.github.v3+json
→ { "token": "<REGISTRATION_TOKEN>", "expires_at": "<ISO8601>" }
```

**Organization runner registration token**:
```
POST /orgs/{org}/actions/runners/registration-token
Authorization: token {PAT}
Accept: application/vnd.github.v3+json
→ { "token": "<REGISTRATION_TOKEN>", "expires_at": "<ISO8601>" }
```

**Runner de-registration (removal)**:
```
DELETE /repos/{owner}/{repo}/actions/runners/{runner_id}   # repo runner
DELETE /orgs/{org}/actions/runners/{runner_id}             # org runner
Authorization: token {PAT}
→ 204 No Content
```

**Required PAT scopes**:
- Repository runners: `repo` scope
- Organization runners: `admin:org` scope (or the narrower `admin:org_hook`)

**Rationale**: The standard documented approach avoids any third-party GitHub client library, keeping
dependencies minimal. The controller fetches a fresh token immediately before passing it to the runner Pod,
so token expiry is not a concern.  
**Alternatives considered**: GitHub App credentials — deferred to a future version per spec assumptions.

---

## Decision 4: Runner Container Image — `ghcr.io/actions/runner`

**Decision**: Use `ghcr.io/actions/runner` (official GitHub-maintained image).

**Configuration environment variables**:

| Variable | Purpose |
|---|---|
| `GITHUB_URL` | Target repository or org URL |
| `RUNNER_TOKEN` | Registration token (fetched by controller) |
| `RUNNER_NAME` | Runner display name (defaults to Pod name) |
| `RUNNER_LABELS` | Comma-separated labels (e.g., `kubernetes,prod`) |
| `RUNNER_GROUP` | Runner group name |
| `RUNNER_ALLOW_RUNASROOT` | Set `true` for container environments |

**Startup behavior**: The image entrypoint automatically calls `./config.sh` on first start using env vars,
then runs `./run.sh` to keep the runner alive. No wrapper entrypoint needed for registration.

**De-registration**: The image does NOT auto-deregister on SIGTERM. Two-layer de-registration strategy:
1. **preStop lifecycle hook** in the Pod spec runs `./config.sh remove --unattended` before Pod termination
   (handles graceful Pod deletion scenarios).
2. **Controller DELETE API call** during finalizer processing (handles cases where the Pod is already gone
   or the preStop hook did not complete).

**Rationale**: Official GitHub-maintained image ensures version alignment with GitHub Actions runner
updates and security patches. Full env-var configuration fits the Kubernetes Secret injection pattern.  
**Alternatives considered**: `myoung34/github-runner` (third-party, widely used but adds dependency);
`summerwind/actions-runner-controller` runner image (community-maintained, designed for ARC not standalone operators).

---

## Decision 5: De-registration Ownership — Finalizer Pattern

**Decision**: Add a Kubernetes finalizer (`idp.grc.io/runner-cleanup`) to every `GitHubRunner` CR on
first reconcile. The finalizer processing block handles:
1. Calling `DELETE /repos/{owner}/{repo}/actions/runners/{runner_id}` (or org equivalent) via the GitHub API
2. Deleting the runner Pod
3. Removing the finalizer to allow CR deletion to complete

**Rationale**: Finalizers are the Kubernetes-idiomatic way to perform cleanup before object deletion.
They ensure de-registration happens even if the Pod has already been deleted (e.g., by node failure).  
**Alternatives considered**: preStop hook only — insufficient because the hook is skipped if Kubernetes
force-deletes a Pod or if the Pod never started.

---

## Decision 6: Credential Secret Format

**Decision**: A standard Kubernetes `Opaque` Secret in the same namespace as the `GitHubRunner` CR,
with a single key `token` containing the GitHub PAT.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: github-pat
  namespace: default
type: Opaque
stringData:
  token: "ghp_..."
```

The CR's spec references the secret by name:
```yaml
spec:
  credentialsSecretRef:
    name: github-pat
```

**Rationale**: Follows the standard Kubernetes Secret reference pattern used by Ingress TLS, image pull
secrets, and other operators. Keeps credentials out of the CR spec.  
**Alternatives considered**: Direct env var injection in the CR — violates Kubernetes security practices;
mounting a GitHub App private key — deferred to future version.

---

## Decision 7: Project Source Layout

**Decision**: Standard kubebuilder v4 layout.

```
cmd/main.go                         # manager entrypoint
api/v1alpha1/
  githubrunner_types.go             # CRD Go types
  groupversion_info.go              # GroupVersion registration
  zz_generated.deepcopy.go         # generated
internal/controller/
  githubrunner_controller.go        # reconciler
  github_client.go                  # GitHub API calls
  suite_test.go                     # envtest setup
  githubrunner_controller_test.go   # controller tests
config/
  crd/                              # generated CRD YAML
  rbac/                             # generated ClusterRole/Binding
  manager/                          # Deployment manifest
  default/                          # kustomization.yaml
  samples/                          # example GitHubRunner CR
Dockerfile
Makefile
go.mod                              # module: github.com/davidsugianto/idp-grc
README.md
```

**Go module**: `github.com/davidsugianto/idp-grc`  
**API group/domain**: `idp.grc.io` (from constitution); CRD group: `idp.grc.io`; version: `v1alpha1`

**Rationale**: kubebuilder v4 layout is the canonical standard for Go operators. Aligns with CLAUDE.md,
constitution, and all upstream tooling expectations.
