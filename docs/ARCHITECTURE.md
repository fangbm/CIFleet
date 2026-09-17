# Architecture

## Components

### Controller

The controller is the only component that should hold long-lived GitHub credentials.
It receives GitHub events, tracks hosted-runner quota policy, maintains worker state,
selects a placement, obtains short-lived JIT runner material, and instructs a worker
agent to launch an ephemeral environment.

### Agent

An agent runs on each physical worker and exposes only the operations needed by the
controller. V1 targets three worker profiles:

- Linux amd64: Docker + KVM/libvirt.
- Linux arm64: Docker.
- Windows amd64: Hyper-V.

### Backends

Backends implement a common lifecycle:

1. Probe capacity.
2. Create an isolated execution environment.
3. Inject short-lived runner configuration.
4. Start the GitHub Actions runner.
5. Report state/logs.
6. Destroy the environment after one job.

Docker is the preferred default for normal Linux builds. KVM/Hyper-V is preferred for
full-OS tests, stronger isolation, Docker-daemon workloads, systemd/kernel tests, and
native Windows toolchains.

## Scheduler model

A job declares requirements:

```text
OS + architecture + isolation + capabilities + CPU + memory
```

A node advertises capabilities and current capacity. The scheduler matches the two and
chooses the least-loaded eligible node. Physical host names are intentionally absent
from repository workflows.

## Hosted runner fallback

V1 should support a central policy state with two modes:

- `hosted`: private repositories use GitHub-hosted runners.
- `self-hosted`: eligible trusted jobs use cifleet labels.

A quota monitor can switch the organization/repository variable before the monthly
included quota is exhausted. Public/untrusted PR workloads should remain hosted by
default.

## Job lifecycle

```text
workflow job queued
      |
      v
controller validates trust/policy
      |
      v
scheduler selects node/backend
      |
      v
controller requests GitHub JIT runner config
      |
      v
agent creates Docker/KVM/Hyper-V instance
      |
      v
one ephemeral runner handles one job
      |
      v
logs/status collected
      |
      v
instance destroyed; repository cache retained
```

## Cache model

Workspaces are disposable. Only explicitly whitelisted cache paths are persistent and
namespaced per repository. Examples include Cargo, pnpm, Gradle, pip/uv, and large model
caches when appropriate.

## Future macOS support

macOS is represented in the model now (`os=macos`, `arch=arm64`) but has no V1 backend.
A future Apple Silicon worker can add a native/virtualization backend without changing
the scheduler contract.
