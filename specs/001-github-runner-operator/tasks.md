---
description: "Task list for GitHub Actions Self-Hosted Runner Operator"
---

# Tasks: GitHub Actions Self-Hosted Runner Operator

**Input**: Design documents from `specs/001-github-runner-operator/`

**Prerequisites**: plan.md ✅ | spec.md ✅ | research.md ✅ | data-model.md ✅ | contracts/ ✅ | quickstart.md ✅

**Tests**: Integration tests included in Polish phase (envtest, no live cluster required for unit tests).

**Organization**: Tasks grouped by user story to enable independent implementation and testing.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no shared dependencies)
- **[Story]**: Which user story the task belongs to
- All tasks include exact file paths

---

## Phase 1: Setup

**Purpose**: Project scaffolding and initialization.

- [ ] T001 Initialize Go module: `go mod init github.com/davidsugianto/idp-grc` in repo root, producing `go.mod`
- [ ] T002 Scaffold kubebuilder v4.1.1 project: `kubebuilder init --domain idp.grc.io --repo github.com/davidsugianto/idp-grc --plugins go/v4` — creates `cmd/main.go`, `Makefile`, `go.sum`, `PROJECT`
- [ ] T003 [P] Create multi-stage `Dockerfile` at repo root: stage 1 builds `cmd/main.go` with `golang:1.22`, stage 2 copies binary to `gcr.io/distroless/static:nonroot`
- [ ] T004 [P] Update `.gitignore` to add `/bin/` and `testbin/` (kubebuilder test binary cache)

---

## Phase 2: Foundational

**Purpose**: Core type definitions, GitHub API client, and manager entrypoint — all user stories depend on these.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [ ] T005 Define `GitHubRunnerSpec` and `GitHubRunnerStatus` Go structs in `api/v1alpha1/githubrunner_types.go` with all fields from `data-model.md` (githubURL, credentialsSecretRef, runnerName, runnerLabels, runnerGroup, image, resources; status: phase, runnerID, runnerName, podName, registeredAt, conditions)
- [ ] T006 [P] Add all kubebuilder markers to `GitHubRunner` type in `api/v1alpha1/githubrunner_types.go`: `+kubebuilder:object:root=true`, `+kubebuilder:subresource:status`, `+kubebuilder:printcolumn` for Phase/Runner-ID/Age, `+kubebuilder:resource:shortName=ghr`
- [ ] T007 [P] Implement `GroupVersion` registration and scheme setup in `api/v1alpha1/groupversion_info.go`
- [ ] T008 Run `make generate` to produce `api/v1alpha1/zz_generated.deepcopy.go`
- [ ] T009 Run `make manifests` to generate CRD YAML in `config/crd/bases/` and RBAC ClusterRole in `config/rbac/`
- [ ] T010 Implement GitHub REST API client in `internal/controller/github_client.go` with four functions: `GetRepoRegistrationToken(ctx, owner, repo, pat)`, `GetOrgRegistrationToken(ctx, org, pat)`, `FindRunnerID(ctx, githubURL, runnerName, pat)`, `DeleteRunner(ctx, githubURL, runnerID, pat)` — using `net/http`, parsing `spec.githubURL` to determine repo vs. org scope per `contracts/github-api.md`
- [ ] T011 Configure manager entrypoint in `cmd/main.go`: register scheme, create manager with leader election enabled (`--leader-elect`), configure health probe on `:8081` (`/healthz`, `/readyz`), configure metrics on `:8080`, register `GitHubRunnerReconciler`, call `mgr.Start`

**Checkpoint**: Foundation complete — all user story phases can now begin.

---

## Phase 3: User Story 1 — Define and Launch a Runner (Priority: P1) 🎯 MVP

**Goal**: Applying a `GitHubRunner` CR creates a runner Pod, registers it with GitHub, and the
CR status reflects the lifecycle from `Pending` → `Registering` → `Running`.
Deleting the CR de-registers from GitHub and removes the Pod.

**Independent Test** (from quickstart.md Scenario 2 & 5): Apply CR with valid PAT Secret →
phase reaches `Running` within 3 min → `kubectl get githubrunners` shows Runner-ID →
delete CR → Pod and GitHub registration removed within 1 min.

### Implementation for User Story 1

- [ ] T012 [US1] Create reconciler struct `GitHubRunnerReconciler` and register it in `internal/controller/githubrunner_controller.go` with kubebuilder RBAC markers for: `githubrunners` (get/list/watch/update/patch), `pods` (get/list/watch/create/delete), `secrets` (get/list/watch), `events` (create/patch)
- [ ] T013 [US1] Implement finalizer add logic in `Reconcile()` in `internal/controller/githubrunner_controller.go`: if CR is not being deleted and finalizer `idp.grc.io/runner-cleanup` is absent, patch it in and requeue
- [ ] T014 [US1] Implement finalizer processing block in `Reconcile()` in `internal/controller/githubrunner_controller.go`: if CR is being deleted, call `DeleteRunner()` from `github_client.go` using `status.runnerID`, delete the runner Pod by name from `status.podName`, then remove the finalizer and patch the CR
- [ ] T015 [US1] Implement `Pending` → `Registering` phase transition in `Reconcile()` in `internal/controller/githubrunner_controller.go`: fetch PAT from Secret referenced in `spec.credentialsSecretRef`, call `GetRepoRegistrationToken()` or `GetOrgRegistrationToken()` based on URL parse, set phase to `Registering`, update status
- [ ] T016 [US1] Implement runner Pod creation in `internal/controller/githubrunner_controller.go`: build Pod spec from `data-model.md` (env vars GITHUB_URL, RUNNER_TOKEN, RUNNER_NAME, RUNNER_LABELS, RUNNER_GROUP, RUNNER_ALLOW_RUNASROOT; preStop lifecycle hook; ownerReference; restartPolicy=Always), create Pod if it does not exist, set `status.podName`
- [ ] T017 [US1] Implement `Registering` → `Running` phase transition in `Reconcile()` in `internal/controller/githubrunner_controller.go`: poll `FindRunnerID()` until runner appears in GitHub's list (with up to 5 retries per reconcile), store returned ID in `status.runnerID`, set `status.runnerName`, set `status.registeredAt`, set phase to `Running`, set `Registered=True` condition
- [ ] T018 [US1] Implement idempotent reconcile guard in `internal/controller/githubrunner_controller.go`: if Pod already exists and phase is `Running`, check Pod readiness and skip re-registration; only re-register if Pod is missing or `status.runnerID` is zero
- [ ] T019 [US1] Implement exponential backoff error handling in `internal/controller/githubrunner_controller.go`: on GitHub API errors, set `Degraded=True` condition with error message, return `ctrl.Result{RequeueAfter: backoff}` (initial 10s, max 5min); on 401/403, set `Failed` phase and do not requeue until Secret changes
- [ ] T020 [P] [US1] Create example `GitHubRunner` CR in `config/samples/idp_v1alpha1_githubrunner.yaml` matching the contract schema in `contracts/githubrunner-cr-schema.yaml` with comments on all fields

**Checkpoint**: US1 complete — apply CR → runner online in GitHub → delete CR → runner gone.

---

## Phase 4: User Story 2 — Observe Runner Status via kubectl (Priority: P2)

**Goal**: `kubectl get githubrunners` shows PHASE and RUNNER-ID columns with accurate values.
`kubectl describe githubrunner <name>` shows conditions with human-readable messages.

**Independent Test** (from quickstart.md Scenario 3): Apply CR with missing Secret → within
60s phase=`Failed`, `Registered=False` condition with message `Secret "x" not found`.
With valid runner: `kubectl get githubrunners` output shows Phase=Running and numeric Runner-ID.

### Implementation for User Story 2

- [ ] T021 [US2] Emit Kubernetes Events in `internal/controller/githubrunner_controller.go`: call `r.Recorder.Event(runner, corev1.EventTypeNormal, "Registered", "Runner registered with GitHub")` on successful registration; `r.Recorder.Event(runner, corev1.EventTypeWarning, "RegistrationFailed", err.Error())` on failure
- [ ] T022 [US2] Implement `PodReady` condition update in `internal/controller/githubrunner_controller.go`: watch the managed Pod's readiness gates; when Pod transitions to Ready, set `PodReady=True`; when Pod becomes NotReady, set `PodReady=False` and requeue to re-register if runnerID is zero
- [ ] T023 [US2] Ensure `Degraded` condition is cleared (`Degraded=False`) in `internal/controller/githubrunner_controller.go` when a previously failing reconcile loop succeeds, and set `Degraded=True` with a descriptive message for all transient GitHub API failures (429, 5xx, timeouts)
- [ ] T024 [US2] Validate printer columns render correctly: run `make manifests` after T006 markers are in place and verify CRD YAML contains `additionalPrinterColumns` for Phase, Runner-ID, and Age in `config/crd/bases/idp.grc.io_githubrunners.yaml`

**Checkpoint**: US2 complete — status visible via kubectl without accessing GitHub UI.

---

## Phase 5: User Story 3 — Install the Operator Into a Real Cluster (Priority: P3)

**Goal**: A single `kubectl apply -f config/default/` installs the CRD, RBAC, and manager
Deployment on a clean cluster without any manual additional steps.

**Independent Test** (from quickstart.md Scenario 1 & 6): Apply `config/default/` → CRD
exists, manager Deployment 1/1 Ready, test GitHubRunner CR reconciled. Re-apply same bundle
→ no existing CRs corrupted.

### Implementation for User Story 3

- [ ] T025 Create manager `Deployment` manifest in `config/manager/manager.yaml`: image placeholder `controller:latest`, liveness probe GET `/healthz:8081`, readiness probe GET `/readyz:8081`, `--leader-elect=true` arg, resource requests (100m CPU, 64Mi memory)
- [ ] T026 [P] Create `ServiceAccount`, `ClusterRole`, `ClusterRoleBinding` manifests in `config/rbac/` with least-privilege verbs matching the `+kubebuilder:rbac:` markers on the reconciler (re-generate with `make manifests` after T012 markers are confirmed)
- [ ] T027 [P] Create `config/default/kustomization.yaml` that composes: `config/crd`, `config/rbac`, `config/manager` — the single install entry point
- [ ] T028 Validate install bundle: apply `config/default/` against a local kind cluster (or equivalent), confirm CRD registered, manager Pod Running, and a sample `GitHubRunner` CR transitions to `Registering` phase (no valid PAT needed for phase transition check — `Failed` phase with credential error is acceptable)

**Checkpoint**: US3 complete — single `kubectl apply` installs the full operator.

---

## Phase 6: User Story 4 — Get Started Without Reading Source Code (Priority: P4)

**Goal**: A developer follows only `README.md` and successfully sees a runner online in
GitHub within 15 minutes, without reading any source code.

**Independent Test** (from quickstart.md Scenario 2 walkthrough): README Prerequisites,
Installation, and Getting Started sections are self-contained and sufficient for a first-time user.

### Implementation for User Story 4

- [ ] T029 [US4] Write `README.md` at repo root with sections: Project Overview, Prerequisites (Kubernetes 1.26+, kubectl, GitHub PAT with scope), Installation (`kubectl apply -f config/default/`), Getting Started (create Secret, apply GitHubRunner CR example, verify with `kubectl get githubrunners`), Troubleshooting (describe output fields, common failure reasons and fixes)
- [ ] T030 [P] [US4] Update `config/samples/idp_v1alpha1_githubrunner.yaml` (from T020) to include a fully commented example matching README instructions, so `kubectl apply -f config/samples/idp_v1alpha1_githubrunner.yaml` (after editing credentials) is the README's suggested first step

**Checkpoint**: US4 complete — README is self-contained for a new user.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Tests, build validation, and end-to-end verification.

- [ ] T031 [P] Set up envtest suite in `internal/controller/suite_test.go`: bootstrap envtest API server with `sigs.k8s.io/controller-runtime/pkg/envtest`, register scheme, start manager
- [ ] T032 [P] Write integration test for happy path in `internal/controller/githubrunner_controller_test.go`: create GitHubRunner CR with fake GitHub API (HTTP test server), verify Pod is created with correct env vars, verify status phase transitions to `Registering`, verify `Registered=True` condition is set
- [ ] T033 Write integration test for deletion/finalizer in `internal/controller/githubrunner_controller_test.go`: create CR, simulate running state, delete CR, verify finalizer processing calls DELETE GitHub endpoint and Pod is removed
- [ ] T034 [P] Write integration test for error surfacing in `internal/controller/githubrunner_controller_test.go`: create CR referencing non-existent Secret, verify phase=`Failed` and `Registered=False` condition within controller re-queue interval
- [ ] T035 [P] Run `go vet ./...` and `go test ./...` — fix any issues
- [ ] T036 Run `make docker-build IMG=ghcr.io/davidsugianto/idp-grc:dev` and confirm image builds without errors
- [ ] T037 Run quickstart.md Scenarios 1–5 on a real cluster (kind or equivalent) to validate end-to-end

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately
- **Foundational (Phase 2)**: Depends on Phase 1 completion — **BLOCKS all user stories**
  - T008 (`make generate`) depends on T005–T007
  - T009 (`make manifests`) depends on T008
  - T011 depends on T010
- **US1 (Phase 3)**: Depends on Phase 2 — no dependencies on US2/US3/US4
- **US2 (Phase 4)**: Depends on Phase 2; T021–T023 depend on US1 (need a working reconciler)
- **US3 (Phase 5)**: Depends on Phase 2; T028 validation benefits from US1+US2 being complete
- **US4 (Phase 6)**: Depends on US3 (install steps must be validated before docs are written)
- **Polish (Phase 7)**: Depends on all user stories being substantially complete

### User Story Dependencies

- **US1 (P1)**: Starts after Phase 2 — no dependency on other stories
- **US2 (P2)**: Starts after Phase 2 — T021–T023 add to the controller built in US1 (can start in parallel if US1 reconciler skeleton T012 is done)
- **US3 (P3)**: Starts after Phase 2 — manifest work is independent of US1/US2 code
- **US4 (P4)**: Starts after US3 (install steps must be verified to exist before docs reference them)

### Within Each User Story

- Foundational setup (markers, manifests) → Types → Client → Reconciler → Status/Events
- T013 and T014 must complete before T015 (finalizer before registration)
- T015 must complete before T016 (token needed before Pod creation)
- T016 must complete before T017 (Pod must exist before runner ID discovery)

### Parallel Opportunities

- T003, T004 — parallel with T002
- T006, T007 — parallel with T005
- T020 — parallel with T012–T019 (sample CR is independent of controller logic)
- T021, T022 — parallel with each other (different event types)
- T025, T026, T027 — parallel (different manifest files)
- T029, T030 — parallel (README and sample CR are independent files)
- T031, T032, T033, T034 — test files are parallel (different test functions)
- T035, T036 — parallel (vet/test vs. docker build)

---

## Parallel Execution Example: Phase 2 Foundational

```bash
# These can start in parallel once Phase 1 is done:
Task T005: "Define GitHubRunner spec/status types in api/v1alpha1/githubrunner_types.go"
Task T006: "Add kubebuilder markers in api/v1alpha1/githubrunner_types.go"  # [P] with T005 if editing same file; sequence after T005
Task T007: "Implement GroupVersion in api/v1alpha1/groupversion_info.go"    # [P] - different file
Task T010: "Implement GitHub client in internal/controller/github_client.go" # [P] - different file

# After T005–T007:
Task T008: "Run make generate → zz_generated.deepcopy.go"

# After T008:
Task T009: "Run make manifests → config/crd/ and config/rbac/"
```

## Parallel Execution Example: User Story 1

```bash
# Once Phase 2 is complete, these start US1 in order:
Task T012: "Create reconciler struct with RBAC markers"
Task T013: "Add finalizer logic"   # depends on T012
Task T014: "Add finalizer cleanup" # depends on T013

# T020 can run in parallel at any point:
Task T020: "Create config/samples/idp_v1alpha1_githubrunner.yaml"  # [P] - independent
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (T001–T004)
2. Complete Phase 2: Foundational (T005–T011) — **blocks everything**
3. Complete Phase 3: User Story 1 (T012–T020)
4. **STOP and VALIDATE**: Apply CR → runner appears online in GitHub → delete CR → runner removed
5. If validation passes: demo, then continue to US2

### Incremental Delivery

1. Setup + Foundational → scaffold complete
2. US1 → runner lifecycle works → **demo: runner online via YAML**
3. US2 → status observable via kubectl
4. US3 → operator installs cleanly
5. US4 → README complete, self-service onboarding works
6. Polish → tests green, image builds, end-to-end verified

### Solo Developer Sequence (No Parallelism)

Phase 1 → Phase 2 → US1 → US2 → US3 → US4 → Polish

---

## Notes

- `[P]` tasks operate on different files — no conflicts when run in parallel
- `[Story]` label maps each task to its user story for traceability
- T008 (`make generate`) and T009 (`make manifests`) are code-generation steps — re-run
  whenever types or markers change
- US1 is the MVP — complete and validate it independently before moving to US2
- The controller's GitHub API calls use `net/http` directly (no SDK) per research.md Decision 3
- De-registration uses both preStop hook (Pod lifecycle) AND finalizer (CR lifecycle) per research.md Decision 5
