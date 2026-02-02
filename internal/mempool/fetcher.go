package mempool

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"blockchain-data-feeds-poc/internal/decode"
	"blockchain-data-feeds-poc/internal/rpc"
	"blockchain-data-feeds-poc/internal/store"
)

const zeroAddress = "0x0000000000000000000000000000000000000000"

type TxHandler interface {
	HandleTx(ctx context.Context, tx store.MempoolTx) error
}

type Fetcher struct {
	rpc         *rpc.Client
	chainID     uint64
	logger      *slog.Logger
	workers     int
	maxInflight int
	decodeInput bool
	decoder     *decode.Registry
	handler     TxHandler
	pendingC    chan string
}

func NewFetcher(rpcClient *rpc.Client, chainID uint64, workers, maxInflight int, decodeInput bool, decoder *decode.Registry, handler TxHandler, logger *slog.Logger) *Fetcher {
	if workers <= 0 {
		workers = 4
	}
	if maxInflight <= 0 {
		maxInflight = 10000
	}
	return &Fetcher{
		rpc:         rpcClient,
		chainID:     chainID,
		logger:      logger,
		workers:     workers,
		maxInflight: maxInflight,
		decodeInput: decodeInput,
		decoder:     decoder,
		handler:     handler,
		pendingC:    make(chan string, maxInflight),
	}
}

func (f *Fetcher) Enqueue(hash string) {
	select {
	case f.pendingC <- hash:
	default:
		f.logger.Warn("pending queue full, dropping hash")
	}
}

func (f *Fetcher) Run(ctx context.Context) {
	for i := 0; i < f.workers; i++ {
		go f.worker(ctx, i)
	}
}

func (f *Fetcher) worker(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		case hash := <-f.pendingC:
			f.handleHash(ctx, hash)
		}
	}
}

func (f *Fetcher) handleHash(ctx context.Context, hash string) {
	var tx *rpc.RPCTransaction
	var err error
	backoff := 200 * time.Millisecond
	for attempt := 0; attempt < 6; attempt++ {
		tx, err = f.rpc.GetTransactionByHash(ctx, hash)
		if err != nil {
			f.logger.Warn("getTransactionByHash failed", slog.String("error", err.Error()))
			return
		}
		if tx != nil {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			backoff *= 2
			if backoff > 2*time.Second {
				backoff = 2 * time.Second
			}
		}
	}
	if tx == nil {
		f.logger.Debug("transaction not found yet", slog.String("hash", hash))
		return
	}

	mempoolTx, err := f.toMempoolTx(tx)
	if err != nil {
		f.logger.Warn("failed to map tx", slog.String("error", err.Error()))
		return
	}

	if f.decodeInput && f.decoder != nil {
		decoded, err := f.decoder.DecodeInput(mempoolTx.To, mempoolTx.Input)
		if err == nil && decoded != nil {
			payload := decode.FormatDecodedInput(decoded)
			if payload != nil {
				b, _ := json.Marshal(payload)
				mempoolTx.DecodedInputJSON = string(b)
			}
		}
	}

	if err := f.handler.HandleTx(ctx, mempoolTx); err != nil {
		f.logger.Warn("handle mempool tx failed", slog.String("error", err.Error()))
	}
}

func (f *Fetcher) toMempoolTx(tx *rpc.RPCTransaction) (store.MempoolTx, error) {
	now := time.Now().UTC()
	to := zeroAddress
	if tx.To != nil && *tx.To != "" {
		to = *tx.To
	}
	value := "0"
	if tx.Value != nil {
		value = tx.Value.ToInt().String()
	}
	gasPrice := ""
	if tx.GasPrice != nil {
		gasPrice = tx.GasPrice.ToInt().String()
	}
	maxFee := ""
	if tx.MaxFeePerGas != nil {
		maxFee = tx.MaxFeePerGas.ToInt().String()
	}
	maxPriority := ""
	if tx.MaxPriorityFeePerGas != nil {
		maxPriority = tx.MaxPriorityFeePerGas.ToInt().String()
	}
	typeVal := uint64(0)
	if tx.TxType != nil {
		typeVal = uint64(*tx.TxType)
	}
	input := tx.Input
	if input == "" {
		input = "0x"
	}
	return store.MempoolTx{
		ChainID:              f.chainID,
		Hash:                 strings.ToLower(tx.Hash),
		From:                 strings.ToLower(tx.From),
		To:                   strings.ToLower(to),
		Nonce:                uint64(tx.Nonce),
		Value:                value,
		Gas:                  uint64(tx.Gas),
		GasPrice:             gasPrice,
		MaxFeePerGas:         maxFee,
		MaxPriorityFeePerGas: maxPriority,
		TxType:               typeVal,
		Input:                input,
		Status:               "pending",
		FirstSeenAt:          now,
		LastSeenAt:           now,
		InsertedAt:           now,
	}, nil
}
