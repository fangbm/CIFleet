# Configuration model

V1 keeps configuration intentionally small. The examples in `configs/` are the target
shape for the eventual YAML loader.

## Node fields

- `id`: stable unique worker name.
- `os`: `linux`, `windows`, future `macos`.
- `arch`: `amd64` or `arm64`.
- `backends`: available execution backends.
- `capabilities`: toolchain or hardware capabilities such as `msvc`, `xcode`, or `gpu`.

## Repository policy fields

A repository policy should express job requirements, trust rules, and cache namespaces.
It should never include long-lived GitHub credentials.

## Secrets

Long-lived GitHub App material belongs only on the controller. Agents receive short-lived
requests authenticated by the controller. Job execution environments receive only the
minimum job-specific credentials required by GitHub Actions.
