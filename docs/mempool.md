# Mempool Feed

## What it is
The mempool is the node’s pool of **unconfirmed** transactions. It is node‑specific and can differ by provider. This feed is best‑effort and should not be treated as final truth.

## How this implementation works

- **WebSocket subscriber** (`eth_subscribe`)
  - `newPendingTransactions` for tx hashes
  - `newHeads` for block notifications
- **Fetcher** (`eth_getTransactionByHash`)
  - Resolves hashes to full transactions
  - Retries when the tx isn’t found yet
  - Stores as `pending`
- **Tracker** (`eth_getTransactionReceipt`)
  - `pending → mined` when receipt exists
  - `pending → replaced` when a new tx from same `from+nonce` appears
  - `pending → dropped` when TTL expires

Decoded input (optional) is stored in `decoded_input_json` within the `mempool_txs` table.

## Run locally with Docker

```bash
docker compose -f local/docker/docker-compose.yml up --build
```

Optional port exposure:

```bash
docker compose -f local/docker/docker-compose.yml -f local/docker/docker-compose.expose.yml up --build
```

UI (when exposed) is available at:

```
http://localhost:8080
```

Use **Load Latest** and **Apply to Config** to wire the deployed token into the logs feed, then start the logs indexer:

```bash
docker compose -f local/docker/docker-compose.yml --profile logs up --build
```

## Run against Sepolia

You need a WebSocket-capable RPC endpoint.

Example config:

```yaml
network: "sepolia"

networks:
  sepolia:
    rpc_url: "https://YOUR_HTTP_RPC"
    chain_id: 11155111

mempool:
  ws_url: "wss://YOUR_WS_RPC"
  http_url: "https://YOUR_HTTP_RPC"
  track_lifecycle: true
```

Start:

```bash
go run ./cmd/indexer index mempool --config configs/config.local.yaml --track
```

## Known limitations

- Mempool visibility is provider‑specific; some nodes omit private transactions.
- Pending txs can disappear without a trace; `dropped` is best‑effort based on TTL.
- Replacement detection relies on observing both txs from the same `from+nonce`.
- UI uses `eth_sendTransaction` and requires unlocked accounts (Hardhat provides these).
