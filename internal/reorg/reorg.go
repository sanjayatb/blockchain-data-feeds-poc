package reorg

import (
	"context"
	"fmt"
	"log/slog"

	"blockchain-data-feeds-poc/internal/rpc"
	"blockchain-data-feeds-poc/internal/store"
)

// Detect checks the latest checkpoint hash against the chain tip.
// If a mismatch is found, it returns the block to rollback from (inclusive).
func Detect(ctx context.Context, rpcClient *rpc.Client, st store.Store, logger *slog.Logger) (uint64, bool, error) {
	cp, err := st.LastCheckpoint(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("load checkpoint: %w", err)
	}
	if cp == nil {
		return 0, false, nil
	}
	hash, err := rpcClient.GetBlockHashByNumber(ctx, cp.EndBlock)
	if err != nil {
		return 0, false, fmt.Errorf("fetch block hash: %w", err)
	}
	if hash == cp.BlockHash {
		return 0, false, nil
	}
	logger.Warn("reorg detected", slog.Uint64("end_block", cp.EndBlock), slog.String("expected", cp.BlockHash), slog.String("actual", hash))
	prev, err := st.CheckpointBefore(ctx, cp.StartBlock)
	if err != nil {
		return 0, false, fmt.Errorf("load previous checkpoint: %w", err)
	}
	rollbackFrom := cp.StartBlock
	if prev != nil {
		rollbackFrom = prev.EndBlock + 1
	}
	return rollbackFrom, true, nil
}

func Rollback(ctx context.Context, st store.Store, fromBlock uint64, logger *slog.Logger) error {
	logger.Warn("rolling back", slog.Uint64("from_block", fromBlock))
	return st.RollbackFrom(ctx, fromBlock)
}
