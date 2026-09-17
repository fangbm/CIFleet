# Roadmap

## M0 - Skeleton

- [x] Controller and agent binaries compile.
- [x] Common node/job/placement model.
- [x] Capability-aware scheduler with tests.
- [x] Docker/KVM/Hyper-V backend interfaces/stubs.
- [x] Heartbeat and scheduling HTTP endpoints.
- [x] V1 architecture and configuration examples.

## M1 - Real node control

- [x] TLS 1.3 mutual authentication with node-ID certificate binding.
- [x] Periodic agent heartbeat with Docker capacity probing and node TTL.
- [x] Docker backend create/destroy API.
- [x] Repository-scoped cache mounts.
- [x] Job timeout and restart-safe orphan cleanup via Docker labels.

See [`M1.md`](M1.md).

## M2 - GitHub integration

- [x] GitHub App JWT and installation-token authentication.
- [x] `workflow_job` webhook receiver with HMAC-SHA256 validation.
- [x] Repository JIT/ephemeral runner configuration.
- [x] One-job Docker runner lifecycle and completed-job cleanup.
- [x] CIFleet label -> OS/architecture/capability mapping.
- [x] Persisted pending state, retries, duplicate-delivery idempotency and tombstones.
- [x] Baseline public fork / `pull_request_target` denial.
- [x] Controller-only identity enforcement on worker control endpoints.

See [`M2.md`](M2.md).

## M3 - VM backends

- [ ] Linux KVM/libvirt backend with qcow2 overlays.
- [ ] Windows Hyper-V backend with differencing disks/checkpoints.
- [ ] Golden image versioning and refresh.

## M4 - Hosted/self-hosted fallback

- [ ] Actions usage/quota reader.
- [ ] Threshold policy with hysteresis.
- [ ] Organization/repository variable switcher.
- [ ] Automatic monthly reset detection.
- [ ] Richer repository/actor trust policy beyond the M2 baseline.

## M5 - Operations

- [ ] SQLite durable state (replace M2 JSON state file).
- [ ] Structured audit log.
- [ ] Prometheus metrics.
- [ ] Web dashboard (optional).
- [ ] Wake-on-LAN for desktop workers.

## V2 candidates

- [ ] macOS arm64 worker.
- [ ] Apple Virtualization Framework backend.
- [ ] GPU capability/resource scheduling.
- [ ] Multiple controllers / HA.
