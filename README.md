# idp-grc

A Kubernetes operator that manages GitHub Actions self-hosted runners inside a cluster.
Declare runners as YAML, apply with `kubectl`, and the controller handles the rest.

## Overview

`idp-grc` lets you define GitHub Actions self-hosted runners as Kubernetes custom resources.
The controller handles the full lifecycle:

- Fetches a registration token from the GitHub API
- Creates a runner Pod in the same namespace as the CR
- Monitors Pod health and re-registers runners automatically if they crash
- De-registers the runner from GitHub and deletes the Pod when the CR is deleted

## Prerequisites

- Kubernetes cluster v1.26 or later
- `kubectl` configured to talk to the cluster
- A GitHub Personal Access Token (PAT) with:
  - `repo` scope for repository runners
  - `admin:org` scope for organization runners

## Installation

Apply the Kustomize bundle once:

```bash
kubectl apply -k config/default/
```

Verify the operator is running:

```bash
kubectl get deployment -n idp-grc-system
# NAME              READY   UP-TO-DATE   AVAILABLE
# idp-grc-manager   1/1     1            1

kubectl get crds githubrunners.idp.grc.io
# NAME                          CREATED AT
# githubrunners.idp.grc.io     ...
```

## Getting Started

### 1. Create a Secret with your GitHub PAT

```bash
kubectl create secret generic github-pat \
  --from-literal=token=<YOUR_GITHUB_PAT> \
  --namespace=default
```

### 2. Define a GitHubRunner

Create a file `my-runner.yaml`:

```yaml
apiVersion: idp.grc.io/v1alpha1
kind: GitHubRunner
metadata:
  name: my-runner
  namespace: default
spec:
  githubURL: "https://github.com/YOUR_ORG/YOUR_REPO"
  credentialsSecretRef:
    name: github-pat
  runnerLabels:
    - kubernetes
    - self-hosted
```

Apply it:

```bash
kubectl apply -f my-runner.yaml
```

### 3. Verify the runner is online

Watch the status (runner should reach `Running` within 3 minutes):

```bash
kubectl get githubrunners -w
# NAME         PHASE         RUNNER-ID   AGE
# my-runner    Pending       0           2s
# my-runner    Registering   0           8s
# my-runner    Running       12345678    45s
```

Check the runner Pod:

```bash
kubectl get pod my-runner-runner
# NAME               READY   STATUS    RESTARTS
# my-runner-runner   1/1     Running   0
```

Then visit `https://github.com/YOUR_ORG/YOUR_REPO/settings/actions/runners` — your runner
should appear as **online**.

### 4. Delete the runner

```bash
kubectl delete githubrunner my-runner
```

The controller de-registers the runner from GitHub and deletes the Pod within 1 minute.

## Troubleshooting

### Runner stays in Pending or Failed

Inspect the status conditions:

```bash
kubectl describe githubrunner my-runner
```

Look at the `Status > Conditions` section:

| Condition    | Meaning |
|-------------|---------|
| `Registered=True` | Runner is registered with GitHub |
| `Registered=False, Reason=CredentialsNotFound` | Secret does not exist in the namespace |
| `Registered=False, Reason=AuthenticationFailed` | PAT is invalid or lacks required scope |
| `PodReady=True` | Runner Pod is ready |
| `Degraded=True` | Transient error — controller is retrying |

Check events on the CR:

```bash
kubectl events --for githubrunner/my-runner
```

### Manager Pod logs

```bash
kubectl logs -n idp-grc-system -l app.kubernetes.io/name=idp-grc
```

## Building from Source

```bash
# Build the binary
go build -o bin/manager ./cmd/...

# Build the container image
make docker-build IMG=ghcr.io/YOUR_ORG/idp-grc:latest

# Push the image
make docker-push IMG=ghcr.io/YOUR_ORG/idp-grc:latest

# Run tests
go test ./...
```

## Architecture

```
GitHubRunner CR
       │
       ▼
 GitHubRunnerReconciler
       │
       ├─▶ Fetch PAT from Secret
       ├─▶ POST /repos/.../actions/runners/registration-token (GitHub API)
       ├─▶ Create runner Pod (env: GITHUB_URL, RUNNER_TOKEN, RUNNER_NAME, ...)
       ├─▶ Poll /repos/.../actions/runners until runner ID appears
       └─▶ Update CR status.phase → Running
```

On CR deletion:
1. Finalizer triggers cleanup
2. `DELETE /repos/.../actions/runners/{id}` (GitHub API)
3. Pod deleted
4. Finalizer removed → CR gone

## License

Apache 2.0
