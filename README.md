# cifleet

`cifleet` is a lightweight heterogeneous GitHub Actions self-hosted CI control plane.
It is designed to use GitHub-hosted runners normally, then route trusted jobs to your
own heterogeneous machines when needed (for example when hosted Actions quota is low).

## V1 topology

```text
                         GitHub Actions
                               |
                        Go Controller
                    (Raspberry Pi / always on)
                               |
              +----------------+----------------+
              |                |                |
        Linux x86_64      Linux arm64      Windows x86_64
              |                |                |
          E5-2666         Raspberry Pi       Windows PC
              |                |                |
        Docker / KVM         Docker           Hyper-V
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

- Go controller.
- Go node agent.
- Linux x86_64 node: Docker + KVM/libvirt backend.
- Linux arm64 node: Docker backend.
- Windows x86_64 node: Hyper-V backend.
- Capability-aware scheduler.
- Node heartbeats and basic health state.
- GitHub webhook / JIT runner integration points.
- Hosted Actions quota policy integration point.

The current initial commit is a compiling architecture skeleton. GitHub App auth,
webhook verification, JIT runner creation, Docker/libvirt/Hyper-V lifecycle operations,
and quota switching are intentionally staged for subsequent milestones.

## Quick start

```bash
go test ./...
go run ./cmd/controller -listen :8080
go run ./cmd/agent -listen :8090 -node-id raspberry
```

Check controller health:

```bash
curl http://localhost:8080/healthz
```

Register/update a worker heartbeat:

```bash
curl -X POST http://localhost:8080/v1/nodes/heartbeat \
  -H 'content-type: application/json' \
  -d '{
    "id":"e5",
    "os":"linux",
    "arch":"amd64",
    "backends":["docker","kvm"],
    "capabilities":["container","vm"],
    "online":true
  }'
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

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) and [docs/ROADMAP.md](docs/ROADMAP.md).
