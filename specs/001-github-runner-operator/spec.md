# Feature Specification: GitHub Actions Self-Hosted Runner Operator

**Feature Branch**: `001-github-runner-operator`

**Created**: 2026-06-30

**Status**: Draft

**Input**: User description: "Build a Kubernetes operator called idp-grc that lets me run and manage GitHub Actions self-hosted runners inside a cluster. I want it to feel like a normal Kubernetes experience, so I can define runners in YAML and have the controller create them, configure them, and keep them running without me doing everything by hand. Please include the custom resources, the controller behavior, and the pieces needed to deploy and run it in a real cluster. Keep it in Go, make it production minded, and set it up so it can be built into a container and installed cleanly. Also add clear docs for getting started."

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Define and Launch a Runner (Priority: P1)

A cluster operator wants to register a GitHub Actions self-hosted runner for a specific repository
or organization. They create a `GitHubRunner` YAML manifest, apply it to the cluster, and within
a short time the runner appears as "online" in GitHub's Actions runner list — without manually
generating registration tokens, running scripts, or interacting with the GitHub UI.

**Why this priority**: This is the foundational value proposition of the operator. All other
stories build on a runner that can be created declaratively.

**Independent Test**: Apply a `GitHubRunner` CR to a test cluster with valid GitHub credentials,
wait for the runner Pod to become Ready, then verify the runner appears online in the target
GitHub repository or organization's Actions settings.

**Acceptance Scenarios**:

1. **Given** a `GitHubRunner` CR referencing a Kubernetes Secret with a GitHub PAT and a target
   repository URL, **When** the CR is applied with `kubectl apply`, **Then** a runner Pod is
   created in the same namespace, the runner registers itself with GitHub, and the CR's
   `.status.phase` transitions to `Running` within 3 minutes.

2. **Given** a `GitHubRunner` CR in `Running` phase, **When** the runner Pod crashes and
   restarts, **Then** the controller re-registers the runner if needed and the `.status.phase`
   returns to `Running` without user intervention.

3. **Given** a `GitHubRunner` CR is deleted with `kubectl delete`, **Then** the runner is
   de-registered from GitHub and the runner Pod is removed within 1 minute.

---

### User Story 2 - Observe Runner Status via kubectl (Priority: P2)

A cluster operator wants to see the current state of all runners at a glance using standard
kubectl commands — without accessing GitHub's UI or reading container logs to determine whether
runners are healthy.

**Why this priority**: Observability via Kubernetes-native tooling is required by the project
constitution's "Kubernetes-Native UX" principle.

**Independent Test**: With one or more `GitHubRunner` CRs applied, run `kubectl get githubrunners`
and `kubectl describe githubrunner <name>` and verify that status fields (phase, conditions,
runner ID, last registration time) reflect the actual state of the runner.

**Acceptance Scenarios**:

1. **Given** a runner that has successfully registered, **When** `kubectl get githubrunners`
   is run, **Then** the output includes columns for PHASE, RUNNER-ID, and AGE with accurate values.

2. **Given** a runner that has failed to register (e.g., invalid PAT), **When**
   `kubectl describe githubrunner <name>` is run, **Then** the `.status.conditions` section
   contains a `Registered` condition with `Status: False` and a human-readable message
   describing the failure reason.

3. **Given** a runner CR, **When** the underlying Pod's readiness changes, **Then** the CR's
   `.status.phase` reflects that change within 30 seconds.

---

### User Story 3 - Install the Operator Into a Real Cluster (Priority: P3)

A platform engineer wants to install `idp-grc` into their Kubernetes cluster by applying a
single manifest bundle — without writing their own Deployment manifests, CRD definitions,
or RBAC rules from scratch.

**Why this priority**: The operator must be installable by someone who has not read the source
code, per the project constitution's production readiness and documentation requirements.

**Independent Test**: On a clean cluster, apply the published install manifest bundle
(`kubectl apply -f install.yaml` or equivalent), then verify that the CRD exists, the
operator Deployment is Running, and a test `GitHubRunner` CR can be reconciled successfully.

**Acceptance Scenarios**:

1. **Given** a cluster with no prior `idp-grc` installation, **When** the install bundle is
   applied once with `kubectl apply`, **Then** the `GitHubRunner` CRD is registered, the
   operator Deployment is Running, and no manual RBAC or Deployment authoring is required.

2. **Given** the operator is installed, **When** a new version's install bundle is applied,
   **Then** the existing `GitHubRunner` CRs are not deleted or corrupted during the upgrade.

3. **Given** the operator is installed, **When** the operator Pod is deleted and rescheduled,
   **Then** it restarts without user intervention and resumes reconciling all `GitHubRunner` CRs.

---

### User Story 4 - Get Started Without Reading Source Code (Priority: P4)

A developer new to the project reads the `README.md` and can, by following the Getting Started
guide alone, install the operator and create their first `GitHubRunner` within 15 minutes.

**Why this priority**: The constitution mandates "clear docs for getting started." Poor docs
block adoption and generate support burden.

**Independent Test**: A person unfamiliar with the codebase follows only the README's Getting
Started section and successfully sees a runner online in GitHub within 15 minutes.

**Acceptance Scenarios**:

1. **Given** the README, **When** a reader follows the Prerequisites section, **Then** they
   know exactly what Kubernetes version, tooling (kubectl, etc.), and GitHub permissions are
   required before they begin.

2. **Given** the README Getting Started guide, **When** a reader follows the steps, **Then**
   they create a Kubernetes Secret for their GitHub credentials, apply a `GitHubRunner` CR
   example, and verify the runner is online — with no steps requiring code changes.

3. **Given** the runner fails to register, **When** the reader runs `kubectl describe
   githubrunner <name>`, **Then** the status output they see matches what the README says
   to look for, enabling self-diagnosis.

---

### Edge Cases

- What happens when the GitHub PAT referenced in the Secret has been revoked or expired?
  The controller MUST surface a clear error condition in the CR status, not crash-loop.
- What happens when the cluster loses connectivity to `api.github.com` temporarily?
  The controller MUST retry with exponential backoff and recover automatically when
  connectivity is restored.
- What happens when a `GitHubRunner` CR is applied to a namespace where the referenced
  Secret does not exist? The controller MUST set a failed condition and not create the Pod
  until the Secret is available.
- What happens when the GitHub API rate limit is hit? The controller MUST respect
  `Retry-After` headers and back off accordingly.

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST provide a `GitHubRunner` Custom Resource Definition (CRD) that
  allows users to declare a GitHub Actions self-hosted runner as a Kubernetes resource.
- **FR-002**: The `GitHubRunner` CR spec MUST accept: target GitHub URL (repository or
  organization), reference to a Kubernetes Secret containing GitHub credentials, optional
  runner labels, and optional runner group name.
- **FR-003**: The controller MUST automatically obtain a runner registration token from the
  GitHub API using the credentials referenced in the CR, without requiring the user to
  generate tokens manually.
- **FR-004**: The controller MUST create a runner Pod in the same namespace as the
  `GitHubRunner` CR, configure it to register with GitHub, and keep it running.
- **FR-005**: The controller MUST remove the runner from GitHub (de-register) and delete the
  runner Pod when the `GitHubRunner` CR is deleted.
- **FR-006**: The controller MUST update `.status.phase` and `.status.conditions` on the CR
  to reflect the current state (Pending, Running, Failed, Terminating).
- **FR-007**: The controller MUST surface runner registration failures as Kubernetes Events
  and CR status conditions with actionable messages.
- **FR-008**: The controller MUST recover from transient GitHub API failures using
  exponential backoff without requiring user intervention.
- **FR-009**: The operator MUST be deployable via a Kustomize bundle that includes the CRD,
  RBAC, and manager Deployment manifests.
- **FR-010**: The operator container image MUST be buildable from source using a standard
  `docker build` or equivalent OCI-compatible build command.
- **FR-011**: The project MUST include a README with a Getting Started section covering
  prerequisites, installation, credential setup, and a working `GitHubRunner` CR example.
- **FR-012**: The operator MUST expose health and readiness endpoints for the manager
  Deployment's probes.

### Key Entities

- **GitHubRunner**: The primary custom resource. Represents one GitHub Actions self-hosted
  runner instance. Key attributes: target GitHub URL, Secret reference for credentials, runner
  labels, runner group. Status: phase, runner ID (assigned by GitHub), conditions,
  last registration timestamp.
- **Runner Pod**: The Kubernetes Pod managed by the controller on behalf of a `GitHubRunner`
  CR. Runs the `actions/runner` container image. Owned by (has an owner reference to) the
  `GitHubRunner` CR.
- **GitHub Credential Secret**: A standard Kubernetes Secret in the same namespace as the CR,
  containing either a Personal Access Token (PAT) or GitHub App credentials. Referenced by
  name from the CR spec.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A user can go from zero to a running GitHub Actions self-hosted runner in the
  cluster by following only the README, within 15 minutes, without reading any source code.
- **SC-002**: A `GitHubRunner` CR applied to the cluster produces a registered, online runner
  in GitHub within 3 minutes under normal network conditions.
- **SC-003**: When a `GitHubRunner` CR is deleted, the runner is de-registered from GitHub
  and the Pod is removed within 1 minute.
- **SC-004**: A `GitHubRunner` whose Pod has restarted recovers to `Running` phase without
  manual intervention within 3 minutes of the Pod becoming Ready again.
- **SC-005**: When GitHub credentials are invalid or missing, the CR's status provides a
  human-readable error message within 60 seconds of the controller's next reconcile.
- **SC-006**: The operator can be fully installed on a fresh cluster using a single
  `kubectl apply` command, with no additional manual steps.
- **SC-007**: The operator's manager Deployment remains Running with no crashes during normal
  operation and recovers automatically within Kubernetes restart policy after transient errors.

---

## Assumptions

- The target Kubernetes cluster runs version 1.26 or later (for stable CRD and server-side
  apply support).
- GitHub credentials are supplied as a Kubernetes Secret (PAT-based); GitHub App credential
  support may be added in a future version.
- The runner container image used in the Pod is the official `ghcr.io/actions/runner` image
  or a compatible derivative; the operator does not bundle the runner binary itself.
- One `GitHubRunner` CR corresponds to exactly one runner Pod (1:1 mapping); horizontal
  scaling / runner pools are out of scope for this version.
- The operator runs in its own dedicated namespace (e.g., `idp-grc-system`) but manages
  `GitHubRunner` CRs across all namespaces.
- GitHub repository-level runners and organization-level runners are both supported via the
  same CR, differentiated by the target URL format.
- No webhook trigger or event-driven autoscaling is in scope for this version; runners are
  always-on.
- mTLS between controller and the runner Pod is not required for v1; standard Kubernetes
  network policies can be applied by the cluster operator if needed.
