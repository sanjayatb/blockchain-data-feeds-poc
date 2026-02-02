package mempool

import (
	"context"
	"sync"
	"testing"
	"time"

	"blockchain-data-feeds-poc/internal/rpc"
	"blockchain-data-feeds-poc/internal/store"
	"log/slog"
)

type storeStub struct {
	mu       sync.Mutex
	replaced map[string]string
	dropped  map[string]bool
	upserted map[string]store.MempoolTx
}

func newStoreStub() *storeStub {
	return &storeStub{
		replaced: make(map[string]string),
		dropped:  make(map[string]bool),
		upserted: make(map[string]store.MempoolTx),
	}
}

func (s *storeStub) Migrate(ctx context.Context) error                                  { return nil }
func (s *storeStub) InsertLogs(ctx context.Context, logs []store.LogRecord) error       { return nil }
func (s *storeStub) SaveRangeCheckpoint(ctx context.Context, cp store.Checkpoint) error { return nil }
func (s *storeStub) LastCheckpoint(ctx context.Context) (*store.Checkpoint, error)      { return nil, nil }
func (s *storeStub) CheckpointBefore(ctx context.Context, block uint64) (*store.Checkpoint, error) {
	return nil, nil
}
func (s *storeStub) RollbackFrom(ctx context.Context, fromBlock uint64) error { return nil }
func (s *storeStub) Close() error                                             { return nil }

func (s *storeStub) UpsertMempoolTx(ctx context.Context, tx store.MempoolTx) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upserted[tx.Hash] = tx
	return nil
}

func (s *storeStub) MarkMempoolMined(ctx context.Context, hash string, mined store.MinedInfo) error {
	return nil
}

func (s *storeStub) MarkMempoolReplaced(ctx context.Context, hash string, replacedBy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replaced[hash] = replacedBy
	return nil
}

func (s *storeStub) MarkMempoolDropped(ctx context.Context, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropped[hash] = true
	return nil
}

func (s *storeStub) ListMempool(ctx context.Context, status string, limit int) ([]store.MempoolTx, error) {
	return nil, nil
}

func (s *storeStub) ListLogs(ctx context.Context, limit int) ([]store.LogRecord, error) {
	return nil, nil
}

func TestTrackerReplacementDetection(t *testing.T) {
	ctx := context.Background()
	st := newStoreStub()
	tracker := NewTracker(&rpc.Client{}, st, 10*time.Minute, 1, 100, slog.Default())

	tx1 := store.MempoolTx{
		Hash:        "0xaaa",
		From:        "0xfrom",
		To:          "0xto",
		Nonce:       1,
		Value:       "1",
		Gas:         21000,
		Input:       "0x",
		Status:      "pending",
		FirstSeenAt: time.Now().UTC(),
		LastSeenAt:  time.Now().UTC(),
		InsertedAt:  time.Now().UTC(),
	}
	tx2 := tx1
	tx2.Hash = "0xbbb"

	if err := tracker.HandleTx(ctx, tx1); err != nil {
		t.Fatalf("handle tx1: %v", err)
	}
	if err := tracker.HandleTx(ctx, tx2); err != nil {
		t.Fatalf("handle tx2: %v", err)
	}

	st.mu.Lock()
	replaced := st.replaced[tx1.Hash]
	st.mu.Unlock()
	if replaced != tx2.Hash {
		t.Fatalf("expected replaced %s got %s", tx2.Hash, replaced)
	}
}

func TestTrackerDroppedTTL(t *testing.T) {
	ctx := context.Background()
	st := newStoreStub()
	tracker := NewTracker(&rpc.Client{}, st, 1*time.Second, 1, 100, slog.Default())

	old := time.Now().Add(-5 * time.Second).UTC()
	tx := store.MempoolTx{
		Hash:        "0xccc",
		From:        "0xfrom",
		To:          "0xto",
		Nonce:       2,
		Value:       "1",
		Gas:         21000,
		Input:       "0x",
		Status:      "pending",
		FirstSeenAt: old,
		LastSeenAt:  old,
		InsertedAt:  old,
	}

	if err := tracker.HandleTx(ctx, tx); err != nil {
		t.Fatalf("handle tx: %v", err)
	}
	tracker.dropExpired(ctx)

	st.mu.Lock()
	dropped := st.dropped[tx.Hash]
	st.mu.Unlock()
	if !dropped {
		t.Fatalf("expected dropped")
	}
}
