# Local Docker Mempool Harness

This folder provides a fully containerized mempool test harness with:
- Hardhat node (automining disabled)
- Transaction spammer (ETH + ERC-20 + replacement tx)
- Optional Postgres
- Optional Go indexer service

## Quick start

From repo root:

```bash
docker compose -f local/docker/docker-compose.yml up --build
```

## Expose Hardhat ports to host (optional)

```bash
docker compose -f local/docker/docker-compose.yml -f local/docker/docker-compose.expose.yml up --build
```

UI will be available at:

```
http://localhost:8080
```

## Expected output

- Hardhat logs show pending transactions and mined blocks
- tx-spammer prints deployed token address and tx hashes
- indexer logs show mempool ingestion and lifecycle updates
- UI shows pending → mined → replaced when you send txs or click "Mine Block"

## View logs feed in UI

To show ERC-20 Transfer logs in the UI, click **Load Latest** then **Apply to Config** in the UI, and run the logs indexer locally or via Docker:

```bash
docker compose -f local/docker/docker-compose.yml --profile logs up --build
```

## Observe pending → mined → replaced

- The spammer disables automine, submits pending txs, mines a block, submits a replacement tx, then mines again.
- The mempool indexer should record:
  - `pending` entries
  - `replaced` for the original tx with the same nonce
  - `mined` for the replacement

## Switch to Postgres

Enable the `postgres` profile and point `database.dsn` accordingly:

```bash
docker compose -f local/docker/docker-compose.yml --profile postgres up --build
```

Example DSN:

```
postgres://indexer:indexer@postgres:5432/indexer?sslmode=disable
```
