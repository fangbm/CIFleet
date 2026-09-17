# Configuration model

CIFleet separates the internal worker control plane from public GitHub webhook ingress.

## Controller: M1 control plane

- `CIFLEET_LISTEN`: mTLS control API listener, default `:8080`.
- `CIFLEET_CA_FILE`: CA used to verify worker certificates.
- `CIFLEET_CERT_FILE` / `CIFLEET_KEY_FILE`: controller certificate/key.
- `CIFLEET_NODE_TTL`: heartbeat age after which a worker is offline.

## Controller: M2 GitHub integration

- `CIFLEET_WEBHOOK_LISTEN`: webhook listener, default `127.0.0.1:8081`; publish through HTTPS.
- `CIFLEET_GITHUB_APP_ISSUER`: GitHub App Client ID (preferred) or App ID. Empty disables M2.
- `CIFLEET_GITHUB_APP_PRIVATE_KEY_FILE`: App RSA private key; controller only.
- `CIFLEET_GITHUB_WEBHOOK_SECRET_FILE`: HMAC webhook secret file.
- `CIFLEET_GITHUB_API_URL`: REST base URL, default `https://api.github.com`.
- `CIFLEET_GITHUB_API_VERSION`: REST version sent by the client.
- `CIFLEET_GITHUB_MARKER_LABEL`: opt-in Actions label, default `cifleet`.
- `CIFLEET_RUNNER_GROUP_ID`: target self-hosted runner group ID.
- `CIFLEET_RUNNER_IMAGE`: Docker runner image.
- `CIFLEET_RUNNER_CPU` / `CIFLEET_RUNNER_MEMORY_MB`: default reservation per runner.
- `CIFLEET_RUNNER_TIMEOUT`: runner-container deadline.
- `CIFLEET_STATE_FILE`: durable JSON job state used for webhook retry/idempotency.

## Agent

- `CIFLEET_NODE_ID`: stable unique worker name; must match the agent certificate CN.
- `CIFLEET_ENDPOINT`: controller-reachable HTTPS URL for this agent.
- `CIFLEET_CONTROLLER_URL`: controller M1 mTLS URL.
- `CIFLEET_CONTROLLER_SERVER_NAME`: optional TLS server-name override for the controller.
- `CIFLEET_CONTROLLER_IDENTITY`: client certificate CN permitted to call worker control APIs, default `controller`.
- `CIFLEET_DOCKER_BIN`: Docker CLI path.
- `CIFLEET_CACHE_ROOT`: repository-scoped cache root.
- `CIFLEET_HEARTBEAT_INTERVAL`: heartbeat cadence.
- `CIFLEET_CLEANUP_INTERVAL`: Docker deadline janitor cadence.
- `CIFLEET_JOB_TIMEOUT`: default container lifetime.
- `CIFLEET_CAPABILITIES` / `CIFLEET_LABELS`: comma-separated advertised metadata.

Long-lived GitHub App material belongs only on the controller. Agents receive only short-lived JIT configuration and never receive the App private key or installation token.
