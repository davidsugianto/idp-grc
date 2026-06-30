# Quickstart Validation Guide: GitHub Actions Self-Hosted Runner Operator

**Feature**: `001-github-runner-operator`
**Date**: 2026-06-30

This guide documents the runnable validation scenarios that prove the operator works end-to-end.
It is not a full tutorial (that is the README); this is the acceptance validation recipe used
by the developer/reviewer to confirm the implementation is correct.

---

## Prerequisites

- A running Kubernetes cluster (1.26+) with `kubectl` configured
- Access to the cluster's `kubeconfig`
- A GitHub repository or organization you control, for which you can generate a PAT
- A GitHub PAT with the appropriate scope:
  - `repo` scope for repository runners
  - `admin:org` scope for organization runners
- The `idp-grc` controller image built and pushed to a registry accessible from the cluster
  (or `make deploy` using the local build)

---

## Scenario 1: Install the Operator (US3 — Single `kubectl apply`)

### Setup

```bash
# Install from the Kustomize bundle
kubectl apply -f config/default/
```

### Expected Outcome

```bash
# CRD registered
kubectl get crds githubrunners.idp.grc.io
# → NAME                          CREATED AT
# → githubrunners.idp.grc.io     2026-06-30T...

# Operator namespace and Deployment exist
kubectl get deployment -n idp-grc-system
# → NAME              READY   UP-TO-DATE   AVAILABLE
# → idp-grc-manager   1/1     1            1

# Manager Pod is Running
kubectl get pods -n idp-grc-system
# → NAME                               READY   STATUS    RESTARTS
# → idp-grc-manager-<hash>             1/1     Running   0
```

**Pass criteria**: CRD registered, manager Deployment 1/1 Ready, no errors in manager logs.

---

## Scenario 2: Create a Runner and Verify Registration (US1 — Primary Flow)

### Setup

```bash
# 1. Create the credential Secret
kubectl create secret generic github-pat \
  --from-literal=token=<YOUR_GITHUB_PAT> \
  --namespace=default

# 2. Apply the GitHubRunner CR
kubectl apply -f - <<EOF
apiVersion: idp.grc.io/v1alpha1
kind: GitHubRunner
metadata:
  name: my-runner
  namespace: default
spec:
  githubURL: "https://github.com/<owner>/<repo>"
  credentialsSecretRef:
    name: github-pat
  runnerLabels:
    - kubernetes
    - validation
EOF
```

### Expected Outcome

```bash
# Phase transitions to Running within 3 minutes
kubectl get githubrunner my-runner -w
# → NAME         PHASE        RUNNER-ID   AGE
# → my-runner    Pending      0           5s
# → my-runner    Registering  0           10s
# → my-runner    Running      12345678    45s

# Runner Pod is created and Ready
kubectl get pod my-runner-runner
# → NAME               READY   STATUS    RESTARTS
# → my-runner-runner   1/1     Running   0

# Status conditions are set
kubectl describe githubrunner my-runner
# → Status:
# →   Phase: Running
# →   Runner ID: 12345678
# →   Conditions:
# →     Type: Registered  Status: True  Reason: RegistrationSucceeded
# →     Type: PodReady    Status: True  Reason: PodIsReady
# →     Type: Degraded    Status: False
```

**GitHub verification** (manual): Navigate to `https://github.com/<owner>/<repo>/settings/actions/runners`
and confirm the runner appears as "online" with labels `kubernetes,validation`.

**Pass criteria**: Phase=Running within 3 minutes, Pod Ready, `Registered=True` condition,
runner visible in GitHub UI.

---

## Scenario 3: Observe Failure with Invalid Credentials (US2 — Error Surfacing)

### Setup

```bash
# Apply a CR referencing a non-existent secret
kubectl apply -f - <<EOF
apiVersion: idp.grc.io/v1alpha1
kind: GitHubRunner
metadata:
  name: bad-runner
  namespace: default
spec:
  githubURL: "https://github.com/<owner>/<repo>"
  credentialsSecretRef:
    name: does-not-exist
EOF
```

### Expected Outcome

```bash
kubectl describe githubrunner bad-runner
# → Status:
# →   Phase: Failed
# →   Conditions:
# →     Type: Registered  Status: False
# →     Reason: CredentialsNotFound
# →     Message: Secret "does-not-exist" not found in namespace "default"
```

**Pass criteria**: Phase=Failed within 60 seconds, `Registered=False` with a human-readable
message, no crash-loop in the manager Pod.

---

## Scenario 4: Runner Self-Heals After Pod Restart (US1 — Recovery)

### Setup

```bash
# With Scenario 2 still running, delete the runner Pod manually
kubectl delete pod my-runner-runner
```

### Expected Outcome

```bash
# Controller re-creates the Pod and runner returns to Running
kubectl get githubrunner my-runner -w
# → NAME        PHASE         RUNNER-ID   AGE
# → my-runner   Registering   0           1m
# → my-runner   Running       12345679    2m30s
```

**Pass criteria**: Phase returns to Running within 3 minutes without user intervention.
Runner may receive a new Runner ID from GitHub after re-registration.

---

## Scenario 5: Delete Runner and Verify De-registration (US1 — Teardown)

### Setup

```bash
# Delete the CR
kubectl delete githubrunner my-runner
```

### Expected Outcome

```bash
# CR is gone within 1 minute
kubectl get githubrunner my-runner
# → Error from server (NotFound): ...

# Pod is gone
kubectl get pod my-runner-runner
# → Error from server (NotFound): ...
```

**GitHub verification** (manual): Navigate to the repository Actions runner settings and
confirm the runner is no longer listed (or listed as "offline" then removed).

**Pass criteria**: CR deleted, Pod deleted, runner de-registered from GitHub within 1 minute.

---

## Scenario 6: Reinstall (Upgrade Path — US3)

### Setup

```bash
# Re-apply the same (or updated) install bundle
kubectl apply -f config/default/
```

### Expected Outcome

- Existing `GitHubRunner` CRs are not deleted or modified
- Manager Deployment rolls out the new version
- After rollout, all runners return to `Running` phase

**Pass criteria**: No existing CRs lost or corrupted; manager returns to 1/1 Ready.

---

## Cleanup

```bash
kubectl delete githubrunner --all -n default
kubectl delete secret github-pat -n default
kubectl delete -f config/default/
```
