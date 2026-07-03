# Contract: GitHub REST API — Runner Management

The controller communicates with the GitHub REST API using standard HTTP calls
(no third-party GitHub SDK). This document defines the API surface the controller depends on.

---

## Authentication

All requests use a Personal Access Token (PAT) passed as a Bearer token:

```
Authorization: token <PAT>
Accept: application/vnd.github.v3+json
```

---

## Endpoint 1: Get Registration Token (Repository Runner)

```
POST https://api.github.com/repos/{owner}/{repo}/actions/runners/registration-token
```

- **When**: Called during the `Registering` phase, before creating the runner Pod
- **Auth scope required**: `repo`
- **Response** (200 OK):
  ```json
  {
    "token": "GHRT...",
    "expires_at": "2026-06-30T13:00:00Z"
  }
  ```
- **Token validity**: 1 hour. The controller passes the token to the Pod immediately; it is
  never stored in etcd or the CR status.

---

## Endpoint 2: Get Registration Token (Organization Runner)

```
POST https://api.github.com/orgs/{org}/actions/runners/registration-token
```

- **When**: Same as Endpoint 1, but for org-scoped runners (when `githubURL` points to an org)
- **Auth scope required**: `admin:org`
- **Response**: Same shape as Endpoint 1

---

## Endpoint 3: List Runners (Repository)

```
GET https://api.github.com/repos/{owner}/{repo}/actions/runners
```

- **When**: Called after Pod becomes Ready to discover the GitHub-assigned `runner_id`
- **Auth scope required**: `repo`
- **Response** (200 OK):
  ```json
  {
    "total_count": 1,
    "runners": [
      {
        "id": 12345678,
        "name": "default-my-runner",
        "os": "linux",
        "status": "online",
        "busy": false,
        "labels": [...]
      }
    ]
  }
  ```
- The controller matches by runner name to find the `id`, stores it in `status.runnerID`.

---

## Endpoint 4: List Runners (Organization)

```
GET https://api.github.com/orgs/{org}/actions/runners
```

- **When**: Same as Endpoint 3 but for org runners
- **Auth scope required**: `admin:org`
- **Response**: Same shape as Endpoint 3

---

## Endpoint 5: Remove Runner (Repository)

```
DELETE https://api.github.com/repos/{owner}/{repo}/actions/runners/{runner_id}
```

- **When**: Called during finalizer processing (CR deletion)
- **Auth scope required**: `repo`
- **Response**: `204 No Content` on success; `404 Not Found` if runner already removed (treat as success)

---

## Endpoint 6: Remove Runner (Organization)

```
DELETE https://api.github.com/orgs/{org}/actions/runners/{runner_id}
```

- **When**: Same as Endpoint 5 but for org runners
- **Auth scope required**: `admin:org`
- **Response**: Same as Endpoint 5

---

## Error Handling Contract

| HTTP Status | Controller Behavior |
|---|---|
| `401 Unauthorized` | Set `Registered=False` condition with message; do not retry until Secret changes |
| `403 Forbidden` | Same as 401 |
| `404 Not Found` (on DELETE) | Treat as success; runner already removed |
| `422 Unprocessable Entity` | Set `Failed` phase; surface reason in condition message |
| `429 Too Many Requests` | Respect `Retry-After` header; re-queue with that delay |
| `5xx` | Exponential backoff retry; set `Degraded=True` condition |
| Network timeout | Exponential backoff retry; set `Degraded=True` condition |

---

## URL Parsing Convention

The controller parses `spec.githubURL` to determine target type and extract owner/repo or org:

| URL Format | Target Type | Extracted Values |
|---|---|---|
| `https://github.com/{owner}/{repo}` | Repository runner | `owner`, `repo` |
| `https://github.com/{org}` | Organization runner | `org` |

The presence of a second path segment distinguishes repo from org scope.
