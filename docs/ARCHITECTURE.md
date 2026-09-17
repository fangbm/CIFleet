# Architecture

## Network planes

CIFleet deliberately separates two trust domains:

```text
Internet / GitHub
      |
      | HTTPS + webhook HMAC
      v
reverse proxy/tunnel
      |
      v
127.0.0.1:8081  Controller webhook ingress

LAN
Agent <==== TLS 1.3 mutual TLS ====> Controller :8080
```

The public webhook listener never replaces the M1 mTLS API. Worker control remains on the private authenticated plane.

## Controller

The controller is the only component that holds long-lived GitHub App credentials. It verifies `workflow_job` webhooks, persists job state, evaluates trust, tracks workers, selects placements, obtains short-lived JIT runner configuration, and instructs an agent to create/destroy an execution environment.

Queued work is persisted before provisioning. This makes a temporarily offline worker a scheduling condition rather than a lost webhook. Duplicate deliveries are absorbed by per-job state and short-lived completion/rejection tombstones.

## Agent

An agent runs on each physical worker. The worker's own certificate identifies its heartbeat to the controller. Conversely, worker control endpoints require the controller certificate identity (CN `controller` by default), so another worker certificate cannot create containers on its peers.

Current worker profiles:

- Linux amd64: Docker now; KVM later.
- Linux arm64: Docker modelled/available as a worker target.
- Windows amd64: Hyper-V planned.
- macOS arm64: future native/virtualization backend.

## GitHub job lifecycle (M2)

```text
workflow_job: queued
      |
      v
verify X-Hub-Signature-256
      |
      v
require cifleet marker label
      |
      v
persist pending state
      |
      v
fetch workflow run / apply trust policy
      |
      v
scheduler selects eligible node/backend
      |
      v
GitHub App installation token (controller memory only)
      |
      v
generate repository JIT runner config
      |
      v
mTLS create-instance request to agent
      |
      v
ephemeral actions-runner container
      |
      v
one GitHub Actions job
      |
      v
workflow_job: completed
      |
      v
destroy container + remove runner record
```

Installation tokens are cached only in controller memory. The worker receives the encoded JIT configuration, not the App private key or installation access token.

## Scheduler model

A job is translated into:

```text
OS + architecture + isolation + capabilities + CPU + memory
```

A node advertises capabilities and current capacity. The scheduler chooses an eligible least-loaded node. M2 also reserves resources from active local job records so two queued webhooks cannot overcommit a node before the next heartbeat updates its reported capacity.

## Isolation and caches

One CI job maps to one ephemeral execution environment. Docker is the M2 Linux implementation. Workspaces disappear with the container. Only explicitly configured, repository-namespaced cache paths persist.

The official Actions runner container is not treated as a security boundary strong enough for arbitrary hostile public PRs. Public fork PRs and `pull_request_target` jobs are rejected by the baseline M2 trust policy. Stronger isolation belongs on VM backends.

## Hosted runner fallback

Hosted/self-hosted quota switching is M4. The intended model is a central repository/organization variable that normally resolves to `ubuntu-latest` and switches to CIFleet labels when hosted usage crosses a configured threshold.
