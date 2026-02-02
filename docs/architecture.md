# Architecture

## Overview

- **RPC client**: JSON-RPC wrapper with retries, backoff, rate limiting, and concurrency control.
- **Extractor**: Backfill worker pool and head follower with adaptive chunking and confirmation handling.
- **Decoder**: ABI-based event decoding using `go-ethereum` ABI utilities.
- **Store**: Normalized log storage with checkpoints for reorg safety.
- **Reorg handling**: Verify stored checkpoints against chain and rollback from the last safe checkpoint.
- **CLI**: Minimal subcommands for backfill, follow, and migrations.

## Local Docker flow diagram

![Local Docker Mempool Flow](assets/local-docker-mempool-flow.png)

## Rust parity notes

The modules are intentionally shaped to map cleanly to Rust:

- `internal/rpc` → `rpc` crate/module using `reqwest` + rate limiter (`governor`) + retry/backoff.
- `internal/extract` → `extract` module with worker pool via `tokio` tasks and bounded channels.
- `internal/decode` → `decode` module using `ethers-core` ABI decode utilities.
- `internal/store` → `store` module using `sqlx` for SQLite/Postgres.
- `internal/reorg` → `reorg` module with checkpoint verification + rollback.
- `cmd/indexer` → `cli` module using `clap`.
