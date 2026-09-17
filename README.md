# cifleet

`cifleet` is a lightweight heterogeneous GitHub Actions self-hosted CI control plane.
It is designed to use GitHub-hosted runners normally, then route trusted jobs to your
own heterogeneous machines when needed (for example when hosted Actions quota is low).

## V1 topology

```text
                         GitHub Actions
                               |
                        Go Controller
                               |
              +----------------+----------------+
              |                |                |
        Linux x86_64      Linux arm64      Windows x86_64

```

macOS is intentionally not part of V1, but the scheduler and backend model leave room
for a future macOS arm64 worker using a native/virtualization backend.

## Design goals

- Capability-based scheduling instead of hard-coding host names in workflows.
- Ephemeral execution environments per CI job.
- Docker by default; VM isolation when a full OS or stronger isolation is required.
- Native ARM64 and Windows workers rather than pretending cross-compilation is equivalent.
- Controller and agents written in Go and deployable as small systemd/Windows services.
- GitHub-hosted -> self-hosted fallback policy controlled centrally.
- Repository-scoped caches without persisting dirty workspaces.

## V1 scope

- Go controller and Go node agent.
- Capability-aware scheduler.
- **M1 complete:** TLS 1.3 mTLS, certificate-bound worker identity, periodic heartbeat,
  Docker capacity probing, stale-node detection, Docker create/destroy, repo-scoped cache
  mounts, and restart-safe deadline/orphan cleanup.
- Linux x86_64: Docker today; KVM/libvirt is M3.
- Linux arm64: Docker path reuses the same agent/backend and will be brought online after
  the first x86_64 Linux path is validated.
- Windows x86_64: Hyper-V is M3.
- GitHub webhook/JIT runner lifecycle is M2.
- Hosted Actions quota fallback is M4.

## Quick start

Run the test suite first:

```bash
go test ./...
go vet ./...
```

For an actual two-node deployment, generate a local CA and node certificates, install the
controller on the Raspberry Pi and the agent on the Linux x86_64 host, then smoke-test Docker
through mTLS. The exact commands are in [docs/M1.md](docs/M1.md).

For localhost-only development without certificates, both binaries support the explicit
`-insecure-http` flag:

```bash
go run ./cmd/controller -insecure-http -listen :8080
go run ./cmd/agent -insecure-http -listen :8090 -node-id e5 \
  -controller-url http://127.0.0.1:8080
```

## Repository policy idea

A repository should describe *requirements*, not a physical host:

```yaml
repository: fangbm/example
jobs:
  default:
    os: linux
    arch: amd64
    isolation: container
  windows-native:
    os: windows
    arch: amd64
    isolation: vm
    capabilities: [msvc]
```

The scheduler then selects an eligible node/backend.

## Security model

Self-hosted CI should be treated as privileged infrastructure. V1 assumes trusted jobs.
Public/untrusted pull requests should remain on GitHub-hosted runners unless a stronger
sandboxing policy is explicitly configured. Long-lived GitHub credentials must stay in
the controller; workers should receive short-lived job-specific material only.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/M1.md](docs/M1.md), and
[docs/ROADMAP.md](docs/ROADMAP.md).
