package mempool

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"blockchain-data-feeds-poc/internal/rpc"
	"blockchain-data-feeds-poc/internal/store"
)

type trackedTx struct {
	Hash      string
	From      string
	Nonce     uint64
	FirstSeen time.Time
	LastSeen  time.Time
	Status    string
}

type Tracker struct {
	rpc            *rpc.Client
	store          store.Store
	logger         *slog.Logger
	droppedTTL     time.Duration
	receiptWorkers int
	maxInflight    int

	mu        sync.RWMutex
	txs       map[string]*trackedTx
	fromNonce map[string]string
	inflight  map[string]struct{}
	receiptC  chan string
}

func NewTracker(rpcClient *rpc.Client, st store.Store, droppedTTL time.Duration, receiptWorkers int, maxInflight int, logger *slog.Logger) *Tracker {
	if droppedTTL <= 0 {
		droppedTTL = 10 * time.Minute
	}
	if receiptWorkers <= 0 {
		receiptWorkers = 4
	}
	if maxInflight <= 0 {
		maxInflight = 10000
	}
	return &Tracker{
		rpc:            rpcClient,
		store:          st,
		logger:         logger,
		droppedTTL:     droppedTTL,
		receiptWorkers: receiptWorkers,
		maxInflight:    maxInflight,
		txs:            make(map[string]*trackedTx),
		fromNonce:      make(map[string]string),
		inflight:       make(map[string]struct{}),
		receiptC:       make(chan string, maxInflight),
	}
}

func (t *Tracker) Start(ctx context.Context) {
	for i := 0; i < t.receiptWorkers; i++ {
		go t.receiptWorker(ctx)
	}
	go t.sweepDropped(ctx)
}

func (t *Tracker) HandleTx(ctx context.Context, tx store.MempoolTx) error {
	now := time.Now().UTC()
	if tx.FirstSeenAt.IsZero() {
		tx.FirstSeenAt = now
	}
	tx.LastSeenAt = now
	if tx.InsertedAt.IsZero() {
		tx.InsertedAt = now
	}
	tx.Status = "pending"

	key := fromNonceKey(tx.From, tx.Nonce)
	var replaced string

	t.mu.Lock()
	if existing, ok := t.fromNonce[key]; ok && existing != tx.Hash {
		replaced = existing
	}
	t.fromNonce[key] = tx.Hash
	if current, ok := t.txs[tx.Hash]; ok {
		current.LastSeen = now
	} else {
		t.txs[tx.Hash] = &trackedTx{Hash: tx.Hash, From: tx.From, Nonce: tx.Nonce, FirstSeen: tx.FirstSeenAt, LastSeen: now, Status: "pending"}
	}
	t.mu.Unlock()

	if replaced != "" {
		_ = t.store.MarkMempoolReplaced(ctx, replaced, tx.Hash)
		t.mu.Lock()
		if prev, ok := t.txs[replaced]; ok {
			prev.Status = "replaced"
		}
		t.mu.Unlock()
	}

	if err := t.store.UpsertMempoolTx(ctx, tx); err != nil {
		return err
	}

	t.enqueueReceipt(tx.Hash)
	return nil
}

func (t *Tracker) OnHead(head Head) {
	t.scheduleReceipts()
}

func (t *Tracker) enqueueReceipt(hash string) {
	t.mu.Lock()
	if _, ok := t.inflight[hash]; ok {
		t.mu.Unlock()
		return
	}
	if len(t.inflight) >= t.maxInflight {
		t.mu.Unlock()
		t.logger.Warn("receipt inflight full, dropping hash")
		return
	}
	t.inflight[hash] = struct{}{}
	t.mu.Unlock()

	select {
	case t.receiptC <- hash:
	default:
		t.mu.Lock()
		delete(t.inflight, hash)
		t.mu.Unlock()
		t.logger.Warn("receipt queue full, dropping hash")
	}
}

func (t *Tracker) scheduleReceipts() {
	t.mu.RLock()
	pending := make([]string, 0, len(t.txs))
	for hash, tx := range t.txs {
		if tx.Status == "pending" {
			pending = append(pending, hash)
		}
	}
	t.mu.RUnlock()

	for _, hash := range pending {
		t.enqueueReceipt(hash)
	}
}

func (t *Tracker) receiptWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case hash := <-t.receiptC:
			t.handleReceipt(ctx, hash)
		}
	}
}

func (t *Tracker) handleReceipt(ctx context.Context, hash string) {
	defer func() {
		t.mu.Lock()
		delete(t.inflight, hash)
		t.mu.Unlock()
	}()

	receipt, err := t.rpc.GetTransactionReceipt(ctx, hash)
	if err != nil {
		t.logger.Warn("getTransactionReceipt failed", slog.String("error", err.Error()))
		return
	}
	if receipt == nil || receipt.BlockNumber == nil || receipt.TransactionIndex == nil {
		return
	}
	mined := store.MinedInfo{
		BlockNumber: uint64(*receipt.BlockNumber),
		BlockHash:   receipt.BlockHash,
		TxIndex:     uint64(*receipt.TransactionIndex),
	}
	if err := t.store.MarkMempoolMined(ctx, hash, mined); err != nil {
		t.logger.Warn("mark mined failed", slog.String("error", err.Error()))
		return
	}

	t.mu.Lock()
	tracked, ok := t.txs[hash]
	if ok {
		delete(t.txs, hash)
		key := fromNonceKey(tracked.From, tracked.Nonce)
		if current, ok := t.fromNonce[key]; ok && current == hash {
			delete(t.fromNonce, key)
		}
	}
	t.mu.Unlock()
}

func (t *Tracker) sweepDropped(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.dropExpired(ctx)
		}
	}
}

func (t *Tracker) dropExpired(ctx context.Context) {
	cutoff := time.Now().UTC().Add(-t.droppedTTL)
	var dropped []string

	t.mu.Lock()
	for hash, tx := range t.txs {
		if tx.Status == "pending" && tx.FirstSeen.Before(cutoff) {
			dropped = append(dropped, hash)
			tx.Status = "dropped"
		}
	}
	t.mu.Unlock()

	for _, hash := range dropped {
		_ = t.store.MarkMempoolDropped(ctx, hash)
	}
}

func fromNonceKey(from string, nonce uint64) string {
	return from + ":" + fmtUint64(nonce)
}

func fmtUint64(v uint64) string {
	const digits = "0123456789"
	if v == 0 {
		return "0"
	}
	buf := make([]byte, 0, 20)
	for v > 0 {
		buf = append(buf, digits[v%10])
		v /= 10
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}
