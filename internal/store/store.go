package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"blockchain-data-feeds-poc/internal/config"
)

type Store interface {
	Migrate(ctx context.Context) error
	InsertLogs(ctx context.Context, logs []LogRecord) error
	SaveRangeCheckpoint(ctx context.Context, cp Checkpoint) error
	LastCheckpoint(ctx context.Context) (*Checkpoint, error)
	CheckpointBefore(ctx context.Context, block uint64) (*Checkpoint, error)
	RollbackFrom(ctx context.Context, fromBlock uint64) error
	UpsertMempoolTx(ctx context.Context, tx MempoolTx) error
	MarkMempoolMined(ctx context.Context, hash string, mined MinedInfo) error
	MarkMempoolReplaced(ctx context.Context, hash string, replacedBy string) error
	MarkMempoolDropped(ctx context.Context, hash string) error
	ListMempool(ctx context.Context, status string, limit int) ([]MempoolTx, error)
	ListLogs(ctx context.Context, limit int) ([]LogRecord, error)
	Close() error
}

type LogRecord struct {
	ChainID         uint64    `json:"chain_id"`
	ContractAddress string    `json:"contract_address"`
	EventName       string    `json:"event_name"`
	BlockNumber     uint64    `json:"block_number"`
	BlockHash       string    `json:"block_hash"`
	TxHash          string    `json:"tx_hash"`
	LogIndex        uint      `json:"log_index"`
	Timestamp       time.Time `json:"timestamp"`
	ArgsJSON        string    `json:"args_json"`
	TopicsJSON      string    `json:"topics_json"`
	DataHex         string    `json:"data_hex"`
	InsertedAt      time.Time `json:"inserted_at"`
}

type Checkpoint struct {
	ID         int64
	StartBlock uint64
	EndBlock   uint64
	BlockHash  string
	InsertedAt time.Time
}

type MempoolTx struct {
	ChainID              uint64    `json:"chain_id"`
	Hash                 string    `json:"hash"`
	From                 string    `json:"from_addr"`
	To                   string    `json:"to_addr"`
	Nonce                uint64    `json:"nonce"`
	Value                string    `json:"value"`
	Gas                  uint64    `json:"gas"`
	GasPrice             string    `json:"gas_price,omitempty"`
	MaxFeePerGas         string    `json:"max_fee_per_gas,omitempty"`
	MaxPriorityFeePerGas string    `json:"max_priority_fee_per_gas,omitempty"`
	TxType               uint64    `json:"tx_type,omitempty"`
	Input                string    `json:"input"`
	Status               string    `json:"status"`
	FirstSeenAt          time.Time `json:"first_seen_at"`
	LastSeenAt           time.Time `json:"last_seen_at"`
	MinedBlockNumber     uint64    `json:"mined_block_number,omitempty"`
	MinedBlockHash       string    `json:"mined_block_hash,omitempty"`
	MinedTxIndex         uint64    `json:"mined_tx_index,omitempty"`
	ReplacedByTxHash     string    `json:"replaced_by_tx_hash,omitempty"`
	InsertedAt           time.Time `json:"inserted_at"`
	DecodedInputJSON     string    `json:"decoded_input_json,omitempty"`
}

type MinedInfo struct {
	BlockNumber uint64
	BlockHash   string
	TxIndex     uint64
}

func New(cfg config.DatabaseConfig) (Store, error) {
	switch cfg.Driver {
	case "sqlite":
		return NewSQLite(cfg.DSN)
	case "postgres":
		return NewPostgres(cfg.DSN)
	default:
		return nil, fmt.Errorf("unsupported database driver %s", cfg.Driver)
	}
}

// shared helpers

func scanCheckpoint(row *sql.Row) (*Checkpoint, error) {
	var cp Checkpoint
	var inserted int64
	if err := row.Scan(&cp.ID, &cp.StartBlock, &cp.EndBlock, &cp.BlockHash, &inserted); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	cp.InsertedAt = time.Unix(inserted, 0).UTC()
	return &cp, nil
}
