# Data Model: GitHub Actions Self-Hosted Runner Operator

**Feature**: `001-github-runner-operator`
**Date**: 2026-06-30

---

## Entity 1: GitHubRunner (Custom Resource)

The primary Kubernetes custom resource. One instance = one self-hosted runner.

**Go type**: `GitHubRunner` in package `api/v1alpha1`  
**API group/version/resource**: `idp.grc.io/v1alpha1/githubrunners`  
**Scope**: Namespaced

### Spec Fields

| Field | Go Type | Required | Description |
|---|---|---|---|
| `githubURL` | `string` | Yes | Full URL of the target GitHub repository or organization (e.g., `https://github.com/myorg/myrepo` or `https://github.com/myorg`) |
| `credentialsSecretRef.name` | `string` | Yes | Name of the `Opaque` Secret in the same namespace containing the `token` key with the GitHub PAT |
| `runnerName` | `string` | No | Override for the runner display name in GitHub. Defaults to `<namespace>-<cr-name>` |
| `runnerLabels` | `[]string` | No | Additional labels to assign to the runner (e.g., `["kubernetes", "prod"]`) |
| `runnerGroup` | `string` | No | Name of the runner group to assign the runner to. Defaults to `Default` |
| `image` | `string` | No | Runner container image. Defaults to `ghcr.io/actions/runner:latest` |
| `resources` | `corev1.ResourceRequirements` | No | CPU/memory requests and limits for the runner Pod |

### Status Fields

| Field | Go Type | Description |
|---|---|---|
| `phase` | `RunnerPhase` (string enum) | Current lifecycle phase: `Pending`, `Registering`, `Running`, `Failed`, `Terminating` |
| `runnerID` | `int64` | GitHub-assigned runner ID (set after successful registration; used for de-registration) |
| `runnerName` | `string` | Actual runner name registered with GitHub |
| `podName` | `string` | Name of the managed runner Pod |
| `registeredAt` | `*metav1.Time` | Timestamp of last successful registration |
| `conditions` | `[]metav1.Condition` | Standard Kubernetes conditions (see below) |

### Status Conditions

| Type | Meaning when True |
|---|---|
| `Registered` | Runner has been successfully registered with GitHub |
| `PodReady` | The runner Pod is Ready (all containers passing readiness probes) |
| `Degraded` | Controller encountered a non-fatal error; recovery is being attempted |

### Phase Transitions

```
(CR created)
     │
     ▼
 Pending ──► Registering ──► Running
                │                │
                ▼                ▼
             Failed           Terminating ──► (CR deleted)
```

- `Pending`: CR created, controller has not yet attempted registration
- `Registering`: Controller is fetching a registration token and creating the runner Pod
- `Running`: Pod is Ready and runner is online with GitHub
- `Failed`: Registration or Pod creation failed; controller will retry with backoff
- `Terminating`: CR deletion received; controller is de-registering from GitHub and deleting Pod

### kubebuilder Markers (on the Go type)

```go
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Runner-ID",type=integer,JSONPath=`.status.runnerID`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:resource:shortName=ghr
// +kubebuilder:validation:XValidation:rule="self.spec.githubURL != ''",message="githubURL is required"
```

### Finalizer

Name: `idp.grc.io/runner-cleanup`  
Added during first successful reconcile. Removed after de-registration completes.

---

## Entity 2: Runner Pod

A standard Kubernetes `Pod` managed by the controller on behalf of a `GitHubRunner` CR.

**Ownership**: Pod has an `OwnerReference` pointing to the `GitHubRunner` CR (controller=true,
blockOwnerDeletion=true). Kubernetes garbage-collects the Pod automatically if the CR is deleted
_after_ the finalizer is removed.

**Key Pod spec fields set by the controller**:

| Field | Value |
|---|---|
| `spec.containers[0].image` | `spec.image` from CR (default: `ghcr.io/actions/runner:latest`) |
| `spec.containers[0].env[GITHUB_URL]` | `spec.githubURL` from CR |
| `spec.containers[0].env[RUNNER_TOKEN]` | Fetched from GitHub API (set as env, not persisted) |
| `spec.containers[0].env[RUNNER_NAME]` | Resolved runner name |
| `spec.containers[0].env[RUNNER_LABELS]` | `spec.runnerLabels` joined with commas |
| `spec.containers[0].env[RUNNER_GROUP]` | `spec.runnerGroup` from CR |
| `spec.containers[0].env[RUNNER_ALLOW_RUNASROOT]` | `"true"` |
| `spec.containers[0].lifecycle.preStop.exec.command` | `["/bin/sh", "-c", "./config.sh remove --unattended"]` |
| `spec.containers[0].resources` | `spec.resources` from CR |
| `spec.restartPolicy` | `Always` |

**Naming convention**: Pod name = `<cr-name>-runner` (e.g., `my-runner-runner`)

---

## Entity 3: GitHub Credential Secret

A standard Kubernetes `Opaque` Secret in the same namespace as the `GitHubRunner` CR.

**Schema**:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: <name-referenced-in-cr>
  namespace: <same-as-cr>
type: Opaque
data:
  token: <base64-encoded-github-pat>
```

**Required key**: `token` — the GitHub Personal Access Token.

**Required PAT scopes**:
- For repository-level runners: `repo`
- For organization-level runners: `admin:org`

The controller reads this Secret during every reconcile that requires a new registration token.
The Secret is never modified by the controller.

---

## Entity 4: Manager Deployment (Operator Infrastructure)

The `Deployment` that runs the `idp-grc` controller manager in the cluster.

**Namespace**: `idp-grc-system` (dedicated, created by the install bundle)

**Key fields**:

| Field | Value |
|---|---|
| Replicas | 1 (leader election handles HA; replicas > 1 supported) |
| Image | The `idp-grc` controller image (built from this repo) |
| `livenessProbe` | GET `/healthz` on port 8081 |
| `readinessProbe` | GET `/readyz` on port 8081 |
| Leader election | Enabled via `--leader-elect` flag |
| Metrics | Exposed on port 8080 (Prometheus scrape) |

---

## Relationships

```
GitHubRunner CR ─(owns)──► Runner Pod
GitHubRunner CR ─(reads)──► GitHub Credential Secret
Controller ─(creates/reconciles)──► GitHubRunner CR status
Controller ─(calls)──► GitHub REST API
```

The `GitHubRunner` CR is the single source of truth for desired state.
The controller reconciles actual state (Pod existence, GitHub registration) toward it.
