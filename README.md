# CIFleet

CIFleet is a lightweight heterogeneous GitHub Actions self-hosted CI control plane. It turns your own Linux, ARM, Windows, and eventually macOS machines into an on-demand runner fleet while keeping GitHub credentials centralized and execution environments ephemeral.

## Current topology

```text
                         GitHub Actions
                               |
                    workflow_job webhook
                               |
                               v
                        Go Controller
                    (Raspberry Pi / 24x7)
                               |
                 mTLS + capability scheduler
                               |
              +----------------+----------------+
              |                |                |
        Linux x86_64      Linux arm64      Windows x86_64
              |                |                |
          E5-2666         Raspberry Pi       Windows PC
              |                |                |
        Docker / KVM         Docker           Hyper-V
          ^ M2 live                           ^ planned
```

M2 is operational for Docker-backed Linux GitHub Actions jobs. KVM/Hyper-V execution, automatic hosted-runner quota switching, and macOS are later milestones.

## Implemented

- Go controller and node agent.
- TLS 1.3 mutual authentication between controller and workers.
- Certificate identity binding in both directions.
- Worker heartbeats, liveness TTL and Docker capacity probing.
- Capability-aware CPU/memory scheduling.
- GitHub App authentication without long-lived PATs.
- HMAC-verified `workflow_job` webhook ingress on a separate public-facing listener.
- Repository-scoped JIT ephemeral GitHub Actions runners.
- One Docker container per CI job with deadline/orphan cleanup.
- Pending-job persistence, retry after worker outage, webhook idempotency and completion tombstones.
- Baseline denial of public fork PR and public `pull_request_target` workloads.
- Repository-scoped persistent cache namespaces; disposable workspaces.

## Run a CIFleet job

After completing [M1](docs/M1.md) and [M2](docs/M2.md), a workflow opts in with labels rather than a host name:

```yaml
jobs:
  test:
    runs-on: [self-hosted, cifleet, linux-x64]
    steps:
      - run: |
          uname -a
          echo "running on $RUNNER_NAME"
```

A manual smoke workflow is included at `.github/workflows/cifleet-smoke.yml`.

## Security model

The Pi controller owns the GitHub App private key. Installation tokens stay in controller memory, and workers receive only short-lived JIT runner material over mTLS. The GitHub webhook endpoint is HMAC-verified and is kept separate from the internal mTLS control plane.

Self-hosted runners execute repository code on infrastructure you own. M2 therefore does not route public fork PRs or public `pull_request_target` jobs to CIFleet. Docker workers also do not receive the host Docker socket by default.

## Roadmap

- **M0** — architecture skeleton: complete.
- **M1** — secure Linux node control and Docker substrate: complete.
- **M2** — GitHub App/webhook/JIT Docker runner lifecycle: complete.
- **M3** — KVM + Hyper-V VM backends.
- **M4** — GitHub-hosted quota monitoring and automatic hosted/self-hosted switching.
- **M5** — SQLite state, metrics, audit logs, Wake-on-LAN and operations.
- **V2** — macOS arm64 / Apple Virtualization Framework, GPU-aware scheduling, HA.

See [Architecture](docs/ARCHITECTURE.md), [Configuration](docs/CONFIG.md), and [Roadmap](docs/ROADMAP.md).
