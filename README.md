# Web3 Wallet API

A secure, microservice-style **Ethereum wallet API written in Go**. It creates
wallets, signs and broadcasts transactions, and checks balances on the **Sepolia
testnet**, with private keys stored encrypted at rest, JWT/OAuth2 authentication,
role-based access control, OpenAPI/Swagger docs, structured logging, a
domain-event stream, and a hardened container image.

> Built as a focused reference service: small enough to read end-to-end, but
> production-shaped in the parts that matter — key custody, auth, resilience,
> and observability.

---

## Features

| Capability | Endpoint | Notes |
|---|---|---|
| Issue access token | `POST /api/v1/auth/token` | OAuth2 client-credentials → JWT |
| Create wallet | `POST /api/v1/wallets` | Key generated + encrypted; never returned |
| List wallets | `GET /api/v1/wallets` | **Admin only** (RBAC) |
| Check balance | `GET /api/v1/wallets/{address}/balance` | Live on-chain read |
| Send transaction | `POST /api/v1/transactions` | Build → sign → broadcast → emit event |
| Liveness / readiness | `GET /healthz`, `GET /readyz` | K8s-style probes |
| API docs | `GET /swagger/index.html` | Generated from code annotations |

## Architecture

```
cmd/server            process entrypoint, config wiring, graceful shutdown
internal/
  config              env-driven configuration with safe defaults
  logging             structured JSON logging (slog)
  auth                JWT issue/verify, OAuth2 client-credentials, RBAC roles
  wallet              keystore-backed key custody + transaction signing
  ethereum            go-ethereum RPC client (balance, nonce, gas, broadcast)
  transaction         build → sign → broadcast → publish pipeline
  events              domain-event contract + broker-ready publisher
  api                 gin router, handlers, middleware (auth, RBAC, logging, recovery)
docs                  generated OpenAPI/Swagger spec
```

The layering is deliberate: **handlers are thin** (parse, delegate, map errors to
HTTP codes) and **all business logic lives in service packages** with no
dependency on gin or net/http, so it's unit-testable without spinning up a
server. Interfaces (`wallet.Signer`, `events.Publisher`) mark the seams where
production backends (HSM/KMS, Kafka/NATS) plug in without touching call sites.

## Security design

Security is the headline of this service, not an afterthought.

- **Private keys encrypted at rest.** Keys are persisted via go-ethereum's
  keystore, which implements the **Web3 Secret Storage Definition**: each key is
  encrypted with AES-128-CTR under a **scrypt**-derived key (N=2¹⁸) and protected
  by a Keccak-256 MAC. This is the same on-disk format geth uses. Plaintext key
  material is decrypted only transiently, inside the keystore, for a single
  signing operation, then re-locked. **The private key is never returned over the
  API.** See [`internal/wallet/service.go`](internal/wallet/service.go).
- **Replay protection.** Transactions are signed with the **EIP-155** signer
  bound to the configured chain ID, so a Sepolia signature can't be replayed on
  mainnet.
- **Stateless auth + RBAC.** OAuth2 client-credentials grant issues short-lived
  **HS256 JWTs** carrying a role claim; middleware gates routes by role. The
  verifier **pins the signing algorithm** to defend against the `alg=none` /
  algorithm-confusion attack (regression-tested).
- **No secrets in the image.** All secrets come from the environment; the service
  **refuses to start in production** if default secrets are detected, and warns
  loudly in development.
- **Minimal attack surface.** The runtime image is **distroless, non-root,
  static** — no shell, no package manager.
- **Defense in depth at the edge:** request-ID correlation, panic recovery that
  never leaks internals, `ReadHeaderTimeout` against slowloris, and input
  validation (address checksums, base-10 wei amounts to avoid float drift).

> **Production note:** this reference uses one service-level passphrase for the
> keystore. In production each tenant's key-encryption secret would come from a
> managed store (AWS KMS, HashiCorp Vault) or an HSM — the `wallet.Signer`
> interface is exactly that swap point.

## Resilience

The RPC node is treated as an **optional dependency**. If it's unreachable at
startup the service still boots; offline capabilities (wallet creation,
signing) keep working, and chain-dependent endpoints return `503` until
connectivity returns. Because go-ethereum's HTTP transport dials lazily, the
client verifies reachability with a real `eth_chainId` round-trip rather than
trusting a successful dial. Shutdown is graceful on `SIGINT`/`SIGTERM`.

## Event-driven design

Wallet and transaction lifecycle events (`wallet.created`,
`transaction.submitted`, `transaction.failed`) are published through a narrow
`events.Publisher` interface. The default `LogPublisher` is a local stand-in for
a message broker (Kafka / NATS / RabbitMQ); swapping in a real broker is a
deployment concern, not a code change — downstream consumers (notifications,
indexing, audit) stay decoupled.

## Quick start

### Run locally (no signup required)

```bash
make run            # or: go run ./cmd/server
```

Then open **http://localhost:8080/swagger/index.html**.

### End-to-end with curl

```bash
# 1. Get a token (OAuth2 client-credentials)
TOKEN=$(curl -s -X POST localhost:8080/api/v1/auth/token \
  -H 'Content-Type: application/json' \
  -d '{"client_id":"demo-client","client_secret":"dev-insecure-client-secret-change-me"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["access_token"])')

# 2. Create a wallet
curl -s -X POST localhost:8080/api/v1/wallets -H "Authorization: Bearer $TOKEN"

# 3. Check a balance (needs RPC connectivity)
curl -s localhost:8080/api/v1/wallets/0x71C7656EC7ab88b098defB751B7401B5f6d8976F/balance \
  -H "Authorization: Bearer $TOKEN"

# 4. Send a transfer (fund the sender with Sepolia faucet ETH first)
curl -s -X POST localhost:8080/api/v1/transactions \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"from":"0x...","to":"0x...","value_wei":"1000000000000000"}'
```

### Run with Docker

```bash
make docker-up      # docker compose up --build
```

## Development

```bash
make test           # run the test suite (race detector on)
make vet            # static analysis
make swag           # regenerate Swagger docs after changing annotations
make build          # static, stripped binary into ./bin
make help           # list all targets
```

Tests are **offline and deterministic** — no network, no live RPC. They cover
keystore round-trips, signature recovery (proving signed transactions verify to
the signing address), JWT issue/verify including the `alg=none` defense, and
transaction validation + graceful-degradation paths.

## Configuration

All configuration is environment-driven; see [`.env.example`](.env.example) for
the full list. Key variables: `ETH_RPC_URL`, `CHAIN_ID`, `KEYSTORE_PASSPHRASE`,
`JWT_SECRET`, `OAUTH_CLIENT_ID`/`OAUTH_CLIENT_SECRET`, `PORT`, `LOG_LEVEL`.

## Tech stack

Go 1.26 · [go-ethereum](https://github.com/ethereum/go-ethereum) ·
[gin](https://github.com/gin-gonic/gin) ·
[golang-jwt](https://github.com/golang-jwt/jwt) ·
[swaggo](https://github.com/swaggo/swag) · Docker (distroless)

## Roadmap / production hardening

These are intentionally out of scope for the reference but are the natural next
steps: KMS/Vault-backed key custody, per-tenant key isolation, a real message
broker behind `events.Publisher`, transaction receipt polling + confirmation
tracking, rate limiting and an API gateway, EIP-1559 dynamic-fee transactions,
and OpenTelemetry tracing.

## License

MIT
