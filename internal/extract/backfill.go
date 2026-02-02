package extract

import (
	"context"
	"fmt"
	"sync"

	"blockchain-data-feeds-poc/internal/config"
	"log/slog"
)

func Backfill(ctx context.Context, cfg *config.Config, deps Deps, fromOverride *uint64, toOverride *uint64) error {
	start := cfg.Indexer.StartBlock
	if fromOverride != nil {
		start = *fromOverride
	}
	end := uint64(0)
	if toOverride != nil {
		end = *toOverride
	} else {
		head, err := deps.RPC.GetBlockNumber(ctx)
		if err != nil {
			return fmt.Errorf("get head: %w", err)
		}
		if head <= cfg.Indexer.Confirmations {
			return fmt.Errorf("head too low for confirmations")
		}
		end = head - cfg.Indexer.Confirmations
	}
	if end < start {
		deps.Logger.Info("no blocks to backfill",
			slog.Uint64("start", start),
			slog.Uint64("end", end),
		)
		return nil
	}

	jobs := make(chan Range)
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup

	for i := 0; i < cfg.Indexer.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			chunker := NewChunker(cfg.Indexer.MinChunkSize, cfg.Indexer.MaxChunkSize, cfg.Indexer.ChunkSize)
			for r := range jobs {
				if err := processRangeAdaptive(ctx, deps, chunker, r.From, r.To); err != nil {
					select {
					case errCh <- err:
						cancel()
					default:
					}
					return
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for cur := start; cur <= end; {
			select {
			case <-ctx.Done():
				return
			default:
			}
			rEnd := cur + cfg.Indexer.ChunkSize - 1
			if rEnd > end {
				rEnd = end
			}
			jobs <- Range{From: cur, To: rEnd}
			cur = rEnd + 1
		}
	}()

	wg.Wait()
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}
