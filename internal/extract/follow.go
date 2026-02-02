package extract

import (
	"context"
	"fmt"
	"time"

	"blockchain-data-feeds-poc/internal/config"
	"blockchain-data-feeds-poc/internal/reorg"
	"log/slog"
)

func Follow(ctx context.Context, cfg *config.Config, deps Deps) error {
	chunker := NewChunker(cfg.Indexer.MinChunkSize, cfg.Indexer.MaxChunkSize, cfg.Indexer.ChunkSize)

	start := cfg.Indexer.StartBlock
	cp, err := deps.Store.LastCheckpoint(ctx)
	if err != nil {
		return fmt.Errorf("load checkpoint: %w", err)
	}
	if cp != nil {
		start = cp.EndBlock + 1
	}

	poll := time.Duration(cfg.Indexer.PollIntervalSeconds) * time.Second
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		rollbackFrom, reorged, err := reorg.Detect(ctx, deps.RPC, deps.Store, deps.Logger)
		if err != nil {
			return err
		}
		if reorged {
			if err := reorg.Rollback(ctx, deps.Store, rollbackFrom, deps.Logger); err != nil {
				return err
			}
			start = rollbackFrom
		}

		head, err := deps.RPC.GetBlockNumber(ctx)
		if err != nil {
			deps.Metrics.AddRPCErrors(1)
			deps.Logger.Warn("failed to get head", slog.String("error", err.Error()))
			time.Sleep(poll)
			continue
		}
		if head <= cfg.Indexer.Confirmations {
			deps.Logger.Info("waiting for confirmations", slog.Uint64("head", head))
			time.Sleep(poll)
			continue
		}
		safeHead := head - cfg.Indexer.Confirmations
		if safeHead < start {
			deps.Metrics.SetLag(0)
			time.Sleep(poll)
			continue
		}
		deps.Metrics.SetLag(safeHead - start)

		if err := processRangeAdaptive(ctx, deps, chunker, start, safeHead); err != nil {
			return err
		}
		start = safeHead + 1
	}
}
