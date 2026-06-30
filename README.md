# Web3 Wallet API

An Ethereum wallet API in Go. It creates wallets, signs and broadcasts
transactions, and reads balances on the Sepolia testnet, with private keys
encrypted at rest, OAuth2/JWT auth, role-based access control, OpenAPI/Swagger
docs, structured logging, a domain-event publisher, and a distroless container
image.

## Endpoints

| Capability | Endpoint | Notes |
|---|---|---|
| Issue access token | `POST /api/v1/auth/token` | OAuth2 client-credentials → JWT |
| Create wallet | `POST /api/v1/wallets` | Key generated + encrypted; never returned |
| List wallets | `GET /api/v1/wallets` | Admin only (RBAC) |
| Check balance | `GET /api/v1/wallets/{address}/balance` | On-chain read |
| Send transaction | `POST /api/v1/transactions` | Build → sign → broadcast → emit event |
| Liveness / readiness | `GET /healthz`, `GET /readyz` | K8s-style probes |
| API docs | `GET /swagger/index.html` | Generated from annotations |

## Layout

```
cmd/server            entrypoint, config wiring, graceful shutdown
internal/
  config              env-driven configuration
  logging             structured JSON logging (slog)
  auth                JWT issue/verify, OAuth2 client-credentials, RBAC
  wallet              keystore-backed key custody + signing
  ethereum            go-ethereum RPC client (balance, nonce, gas, broadcast)
  transaction         build → sign → broadcast → publish pipeline
  events              domain-event contract + publisher
  api                 gin router, handlers, middleware
docs                  generated OpenAPI/Swagger spec
```

Handlers parse input, delegate, and map errors to status codes; business logic
lives in the service packages with no dependency on gin or net/http, so it's
testable without a server. The `wallet.Signer` and `events.Publisher` interfaces
are where an HSM/KMS or a real broker (Kafka/NATS) replace the defaults.

## Security

- **Keys encrypted at rest.** go-ethereum's keystore (Web3 Secret Storage:
  scrypt-derived AES-128-CTR with a Keccak-256 MAC). Keys are decrypted only
  transiently for a single signing operation and are never returned over the API.
- **Replay protection.** Transactions use the EIP-155 signer bound to the
  configured chain ID, so a Sepolia signature can't be replayed on mainnet.
- **Auth + RBAC.** OAuth2 client-credentials issues short-lived HS256 JWTs with a
  role claim; the verifier pins the algorithm to reject `alg=none` (tested).
  Client credentials are compared in constant time.
- **No default secrets in production.** The service refuses to start in
  production if default secrets are detected, and warns in development.
- **Hardened image.** Distroless, non-root, static — no shell or package manager.
- **Edge hardening.** Request-ID correlation, panic recovery, slowloris
  `ReadHeaderTimeout`, address-checksum and base-10 wei validation.

## Transactions

- **EIP-1559 dynamic fees.** Transfers are typed `DynamicFeeTx`: the priority
  fee comes from `eth_maxPriorityFeePerGas` and the fee cap is sized at
  `2·baseFee + tip` to absorb base-fee growth over a few blocks.
- **Concurrent-safe nonces.** A per-sender nonce manager seeds from the chain's
  pending nonce and allocates locally, so several sends from one account don't
  collide on a nonce. A failed send resets the sender so the next allocation
  re-syncs with the chain instead of leaving a gap.

## Resilience

The RPC node is an optional dependency: if it's unreachable at startup the
service still boots, offline features (wallet creation, signing) keep working,
and chain-dependent endpoints return `503` until it recovers. Because
go-ethereum dials lazily, the client confirms reachability with an `eth_chainId`
round-trip rather than trusting a successful dial. Shutdown is graceful on
`SIGINT`/`SIGTERM`.

## Quick start

```bash
make run            # or: go run ./cmd/server
```

Then open http://localhost:8080/swagger/index.html.

```bash
# Get a token
TOKEN=$(curl -s -X POST localhost:8080/api/v1/auth/token \
  -H 'Content-Type: application/json' \
  -d '{"client_id":"demo-client","client_secret":"dev-insecure-client-secret-change-me"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["access_token"])')

# Create a wallet
curl -s -X POST localhost:8080/api/v1/wallets -H "Authorization: Bearer $TOKEN"

# Check a balance (needs RPC connectivity)
curl -s localhost:8080/api/v1/wallets/0x71C7656EC7ab88b098defB751B7401B5f6d8976F/balance \
  -H "Authorization: Bearer $TOKEN"

# Send a transfer (fund the sender from a Sepolia faucet first)
curl -s -X POST localhost:8080/api/v1/transactions \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"from":"0x...","to":"0x...","value_wei":"1000000000000000"}'
```

With Docker: `make docker-up`.

## Development

```bash
make test           # race detector on
make vet
make swag           # regenerate Swagger docs after changing annotations
make build          # static binary into ./bin
```

Tests are offline and deterministic. They cover keystore round-trips, signature
recovery (signed transactions verify back to the signing address), JWT
issue/verify including the `alg=none` defense, and transaction validation and
degradation paths.

## Configuration

Environment-driven; see [.env.example](.env.example). Key variables:
`ETH_RPC_URL`, `CHAIN_ID`, `KEYSTORE_PASSPHRASE`, `JWT_SECRET`,
`OAUTH_CLIENT_ID`/`OAUTH_CLIENT_SECRET`, `PORT`, `LOG_LEVEL`.

## Stack

Go 1.26 · go-ethereum · gin · golang-jwt · swaggo · Docker (distroless)

## Roadmap

KMS/Vault key custody, per-tenant key isolation, a real broker behind
`events.Publisher`, receipt polling + confirmation tracking, rate limiting and
an API gateway, EIP-1559 dynamic-fee transactions, OpenTelemetry tracing.

## License

MIT
