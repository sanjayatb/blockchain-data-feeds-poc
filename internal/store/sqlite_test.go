package store

import (
	"context"
	"testing"
	"time"
)

func TestSQLiteIdempotentInsert(t *testing.T) {
	ctx := context.Background()
	st, err := NewSQLite("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	log := LogRecord{
		ChainID:         1,
		ContractAddress: "0x111",
		EventName:       "Transfer",
		BlockNumber:     10,
		BlockHash:       "0xabc",
		TxHash:          "0xtx",
		LogIndex:        1,
		Timestamp:       time.Unix(1, 0).UTC(),
		ArgsJSON:        "{}",
		TopicsJSON:      "[]",
		DataHex:         "0x",
		InsertedAt:      time.Unix(1, 0).UTC(),
	}
	if err := st.InsertLogs(ctx, []LogRecord{log}); err != nil {
		t.Fatalf("insert logs: %v", err)
	}
	if err := st.InsertLogs(ctx, []LogRecord{log}); err != nil {
		t.Fatalf("insert logs again: %v", err)
	}
	var count int
	row := st.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM logs")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row got %d", count)
	}
}

func TestSQLiteMempoolUpsert(t *testing.T) {
	ctx := context.Background()
	st, err := NewSQLite("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	first := time.Unix(10, 0).UTC()
	second := time.Unix(20, 0).UTC()
	tx := MempoolTx{
		ChainID:     1,
		Hash:        "0xhash",
		From:        "0xfrom",
		To:          "0xto",
		Nonce:       1,
		Value:       "1",
		Gas:         21000,
		GasPrice:    "100",
		Input:       "0x",
		Status:      "pending",
		FirstSeenAt: first,
		LastSeenAt:  first,
		InsertedAt:  first,
	}

	if err := st.UpsertMempoolTx(ctx, tx); err != nil {
		t.Fatalf("upsert mempool: %v", err)
	}
	tx.LastSeenAt = second
	if err := st.UpsertMempoolTx(ctx, tx); err != nil {
		t.Fatalf("upsert mempool second: %v", err)
	}

	var lastSeen int64
	row := st.db.QueryRowContext(ctx, "SELECT last_seen_at FROM mempool_txs WHERE tx_hash = ?", tx.Hash)
	if err := row.Scan(&lastSeen); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if lastSeen != second.Unix() {
		t.Fatalf("expected last_seen_at %d got %d", second.Unix(), lastSeen)
	}
}
