package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLite(dsn string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Migrate(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS logs (
			chain_id INTEGER NOT NULL,
			contract_address TEXT NOT NULL,
			event_name TEXT NOT NULL,
			block_number INTEGER NOT NULL,
			block_hash TEXT NOT NULL,
			tx_hash TEXT NOT NULL,
			log_index INTEGER NOT NULL,
			timestamp INTEGER NOT NULL,
			args_json TEXT NOT NULL,
			topics_json TEXT NOT NULL,
			data_hex TEXT NOT NULL,
			inserted_at INTEGER NOT NULL,
			PRIMARY KEY (chain_id, tx_hash, log_index)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_block_number ON logs(block_number);`,
		`CREATE TABLE IF NOT EXISTS checkpoints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			start_block INTEGER NOT NULL,
			end_block INTEGER NOT NULL,
			block_hash TEXT NOT NULL,
			inserted_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_checkpoints_end_block ON checkpoints(end_block);`,
		`CREATE TABLE IF NOT EXISTS mempool_txs (
			chain_id INTEGER NOT NULL,
			tx_hash TEXT PRIMARY KEY,
			from_addr TEXT NOT NULL,
			to_addr TEXT NOT NULL,
			nonce INTEGER NOT NULL,
			value TEXT NOT NULL,
			gas INTEGER NOT NULL,
			gas_price TEXT NULL,
			max_fee_per_gas TEXT NULL,
			max_priority_fee_per_gas TEXT NULL,
			tx_type INTEGER NULL,
			input TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('pending','mined','replaced','dropped')),
			first_seen_at DATETIME NOT NULL,
			last_seen_at DATETIME NOT NULL,
			mined_block_number INTEGER NULL,
			mined_block_hash TEXT NULL,
			mined_tx_index INTEGER NULL,
			replaced_by_tx_hash TEXT NULL,
			decoded_input_json TEXT NULL,
			inserted_at DATETIME NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_mempool_from_nonce ON mempool_txs(from_addr, nonce);`,
		`CREATE INDEX IF NOT EXISTS idx_mempool_status ON mempool_txs(status);`,
		`CREATE INDEX IF NOT EXISTS idx_mempool_first_seen ON mempool_txs(first_seen_at);`,
	}
	for _, q := range queries {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

func (s *SQLiteStore) InsertLogs(ctx context.Context, logs []LogRecord) error {
	if len(logs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO logs (
			chain_id, contract_address, event_name, block_number, block_hash,
			tx_hash, log_index, timestamp, args_json, topics_json, data_hex, inserted_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(chain_id, tx_hash, log_index) DO NOTHING;`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, l := range logs {
		_, err := stmt.ExecContext(ctx,
			l.ChainID, l.ContractAddress, l.EventName, l.BlockNumber, l.BlockHash,
			l.TxHash, l.LogIndex, l.Timestamp.Unix(), l.ArgsJSON, l.TopicsJSON, l.DataHex, l.InsertedAt.Unix(),
		)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) SaveRangeCheckpoint(ctx context.Context, cp Checkpoint) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO checkpoints (start_block, end_block, block_hash, inserted_at)
		VALUES (?, ?, ?, ?);`, cp.StartBlock, cp.EndBlock, cp.BlockHash, cp.InsertedAt.Unix())
	return err
}

func (s *SQLiteStore) LastCheckpoint(ctx context.Context) (*Checkpoint, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, start_block, end_block, block_hash, inserted_at
		FROM checkpoints
		ORDER BY end_block DESC
		LIMIT 1;`)
	return scanCheckpoint(row)
}

func (s *SQLiteStore) CheckpointBefore(ctx context.Context, block uint64) (*Checkpoint, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, start_block, end_block, block_hash, inserted_at
		FROM checkpoints
		WHERE end_block < ?
		ORDER BY end_block DESC
		LIMIT 1;`, block)
	return scanCheckpoint(row)
}

func (s *SQLiteStore) RollbackFrom(ctx context.Context, fromBlock uint64) error {
	queries := []string{
		`DELETE FROM logs WHERE block_number >= ?;`,
		`DELETE FROM checkpoints WHERE start_block >= ?;`,
	}
	for _, q := range queries {
		if _, err := s.db.ExecContext(ctx, q, fromBlock); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *SQLiteStore) Now() time.Time {
	return time.Now().UTC()
}

func (s *SQLiteStore) UpsertMempoolTx(ctx context.Context, tx MempoolTx) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mempool_txs (
			chain_id, tx_hash, from_addr, to_addr, nonce, value, gas, gas_price,
			max_fee_per_gas, max_priority_fee_per_gas, tx_type, input, status,
			first_seen_at, last_seen_at, mined_block_number, mined_block_hash,
			mined_tx_index, replaced_by_tx_hash, decoded_input_json, inserted_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tx_hash) DO UPDATE SET
			last_seen_at = excluded.last_seen_at,
			status = excluded.status,
			gas_price = excluded.gas_price,
			max_fee_per_gas = excluded.max_fee_per_gas,
			max_priority_fee_per_gas = excluded.max_priority_fee_per_gas,
			tx_type = excluded.tx_type,
			decoded_input_json = excluded.decoded_input_json;`,
		tx.ChainID, tx.Hash, tx.From, tx.To, tx.Nonce, tx.Value, tx.Gas, nullIfEmpty(tx.GasPrice),
		nullIfEmpty(tx.MaxFeePerGas), nullIfEmpty(tx.MaxPriorityFeePerGas), nullIfZero(tx.TxType),
		tx.Input, tx.Status, tx.FirstSeenAt.Unix(), tx.LastSeenAt.Unix(),
		nullIfZero(tx.MinedBlockNumber), nullIfEmpty(tx.MinedBlockHash), nullIfZero(tx.MinedTxIndex),
		nullIfEmpty(tx.ReplacedByTxHash), nullIfEmpty(tx.DecodedInputJSON), tx.InsertedAt.Unix(),
	)
	return err
}

func (s *SQLiteStore) MarkMempoolMined(ctx context.Context, hash string, mined MinedInfo) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE mempool_txs
		SET status = 'mined',
			mined_block_number = ?,
			mined_block_hash = ?,
			mined_tx_index = ?,
			last_seen_at = ?
		WHERE tx_hash = ?;`,
		mined.BlockNumber, mined.BlockHash, mined.TxIndex, time.Now().UTC().Unix(), hash,
	)
	return err
}

func (s *SQLiteStore) MarkMempoolReplaced(ctx context.Context, hash string, replacedBy string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE mempool_txs
		SET status = 'replaced',
			replaced_by_tx_hash = ?,
			last_seen_at = ?
		WHERE tx_hash = ?;`,
		replacedBy, time.Now().UTC().Unix(), hash,
	)
	return err
}

func (s *SQLiteStore) MarkMempoolDropped(ctx context.Context, hash string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE mempool_txs
		SET status = 'dropped',
			last_seen_at = ?
		WHERE tx_hash = ?;`,
		time.Now().UTC().Unix(), hash,
	)
	return err
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullIfZero(v uint64) any {
	if v == 0 {
		return nil
	}
	return v
}

func (s *SQLiteStore) ListMempool(ctx context.Context, status string, limit int) ([]MempoolTx, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
		SELECT chain_id, tx_hash, from_addr, to_addr, nonce, value, gas, gas_price,
			max_fee_per_gas, max_priority_fee_per_gas, tx_type, input, status,
			first_seen_at, last_seen_at, mined_block_number, mined_block_hash,
			mined_tx_index, replaced_by_tx_hash, decoded_input_json, inserted_at
		FROM mempool_txs`
	args := []any{}
	if status != "" && status != "all" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY last_seen_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MempoolTx{}
	for rows.Next() {
		var tx MempoolTx
		var gasPrice, maxFee, maxPriority, minedHash, replacedBy, decoded sql.NullString
		var txType, minedBlockNumber, minedTxIndex sql.NullInt64
		var firstSeen, lastSeen, inserted int64
		if err := rows.Scan(
			&tx.ChainID, &tx.Hash, &tx.From, &tx.To, &tx.Nonce, &tx.Value, &tx.Gas,
			&gasPrice, &maxFee, &maxPriority, &txType, &tx.Input, &tx.Status,
			&firstSeen, &lastSeen, &minedBlockNumber, &minedHash, &minedTxIndex,
			&replacedBy, &decoded, &inserted,
		); err != nil {
			return nil, err
		}
		tx.GasPrice = gasPrice.String
		tx.MaxFeePerGas = maxFee.String
		tx.MaxPriorityFeePerGas = maxPriority.String
		if txType.Valid {
			tx.TxType = uint64(txType.Int64)
		}
		tx.FirstSeenAt = time.Unix(firstSeen, 0).UTC()
		tx.LastSeenAt = time.Unix(lastSeen, 0).UTC()
		if minedBlockNumber.Valid {
			tx.MinedBlockNumber = uint64(minedBlockNumber.Int64)
		}
		tx.MinedBlockHash = minedHash.String
		if minedTxIndex.Valid {
			tx.MinedTxIndex = uint64(minedTxIndex.Int64)
		}
		tx.ReplacedByTxHash = replacedBy.String
		tx.DecodedInputJSON = decoded.String
		tx.InsertedAt = time.Unix(inserted, 0).UTC()
		out = append(out, tx)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ListLogs(ctx context.Context, limit int) ([]LogRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT chain_id, contract_address, event_name, block_number, block_hash,
			tx_hash, log_index, timestamp, args_json, topics_json, data_hex, inserted_at
		FROM logs
		ORDER BY block_number DESC, log_index DESC
		LIMIT ?;`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []LogRecord{}
	for rows.Next() {
		var r LogRecord
		var ts, inserted int64
		if err := rows.Scan(
			&r.ChainID, &r.ContractAddress, &r.EventName, &r.BlockNumber, &r.BlockHash,
			&r.TxHash, &r.LogIndex, &ts, &r.ArgsJSON, &r.TopicsJSON, &r.DataHex, &inserted,
		); err != nil {
			return nil, err
		}
		r.Timestamp = time.Unix(ts, 0).UTC()
		r.InsertedAt = time.Unix(inserted, 0).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}
