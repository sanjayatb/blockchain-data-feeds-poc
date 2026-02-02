package mempool

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"blockchain-data-feeds-poc/internal/config"
	"blockchain-data-feeds-poc/internal/decode"
	"blockchain-data-feeds-poc/internal/rpc"
	"blockchain-data-feeds-poc/internal/store"
)

type Service struct {
	cfg     *config.Config
	logger  *slog.Logger
	rpcHTTP *rpc.Client
	decoder *decode.Registry
	store   store.Store
}

func NewService(cfg *config.Config, rpcHTTP *rpc.Client, decoder *decode.Registry, store store.Store, logger *slog.Logger) *Service {
	return &Service{cfg: cfg, rpcHTTP: rpcHTTP, decoder: decoder, store: store, logger: logger}
}

func (s *Service) Run(ctx context.Context, decodeInput bool, track bool, droppedTTL string, pendingWorkers int, receiptWorkers int) error {
	mcfg := s.cfg.Mempool
	if decodeInput {
		mcfg.DecodeInput = true
	}
	if pendingWorkers > 0 {
		mcfg.PendingWorkers = pendingWorkers
	}
	if receiptWorkers > 0 {
		mcfg.ReceiptWorkers = receiptWorkers
	}
	if droppedTTL != "" {
		d, err := time.ParseDuration(droppedTTL)
		if err != nil {
			return err
		}
		mcfg.DroppedTTL = d
	}
	if track {
		mcfg.TrackLifecycle = true
	}

	if mcfg.WSURL == "" || mcfg.HTTPURL == "" {
		return fmt.Errorf("mempool.ws_url and mempool.http_url are required")
	}

	pendingC := make(chan string, mcfg.MaxInflightHashes)
	headC := make(chan Head, 128)

	sub := NewSubscriber(mcfg.WSURL, s.logger, pendingC, headC)
	tracker := NewTracker(s.rpcHTTP, s.store, mcfg.DroppedTTL, mcfg.ReceiptWorkers, mcfg.MaxInflightHashes, s.logger)
	fetcher := NewFetcher(s.rpcHTTP, s.cfg.Chain.ChainID, mcfg.PendingWorkers, mcfg.MaxInflightHashes, mcfg.DecodeInput, s.decoder, tracker, s.logger)

	tracker.Start(ctx)
	fetcher.Run(ctx)
	go sub.Run(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case hash := <-pendingC:
			fetcher.Enqueue(hash)
		case head := <-headC:
			if mcfg.TrackLifecycle {
				tracker.OnHead(head)
			}
		}
	}
}
