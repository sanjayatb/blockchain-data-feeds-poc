package metrics

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

type Metrics struct {
	processedBlocks uint64
	processedLogs   uint64
	rpcErrors       uint64
	lagBlocks       uint64
}

func (m *Metrics) AddBlocks(n uint64) {
	atomic.AddUint64(&m.processedBlocks, n)
}

func (m *Metrics) AddLogs(n uint64) {
	atomic.AddUint64(&m.processedLogs, n)
}

func (m *Metrics) AddRPCErrors(n uint64) {
	atomic.AddUint64(&m.rpcErrors, n)
}

func (m *Metrics) SetLag(n uint64) {
	atomic.StoreUint64(&m.lagBlocks, n)
}

func (m *Metrics) Snapshot() (blocks, logs, rpcErrors, lag uint64) {
	return atomic.LoadUint64(&m.processedBlocks), atomic.LoadUint64(&m.processedLogs), atomic.LoadUint64(&m.rpcErrors), atomic.LoadUint64(&m.lagBlocks)
}

func (m *Metrics) StartLogger(ctx context.Context, logger *slog.Logger, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				blocks, logs, rpcErrors, lag := m.Snapshot()
				logger.Info("metrics",
					slog.Uint64("blocks", blocks),
					slog.Uint64("logs", logs),
					slog.Uint64("rpc_errors", rpcErrors),
					slog.Uint64("lag_blocks", lag),
				)
			}
		}
	}()
}
