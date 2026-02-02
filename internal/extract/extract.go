package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"blockchain-data-feeds-poc/internal/decode"
	"blockchain-data-feeds-poc/internal/metrics"
	"blockchain-data-feeds-poc/internal/rpc"
	"blockchain-data-feeds-poc/internal/store"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

type Deps struct {
	RPC     *rpc.Client
	Store   store.Store
	Decoder *decode.Registry
	Logger  *slog.Logger
	Metrics *metrics.Metrics
	ChainID uint64
}

type Range struct {
	From uint64
	To   uint64
}

type BlockCache struct {
	values map[uint64]*rpc.BlockHeader
}

func NewBlockCache() *BlockCache {
	return &BlockCache{values: make(map[uint64]*rpc.BlockHeader)}
}

func (c *BlockCache) Get(ctx context.Context, rpcClient *rpc.Client, number uint64) (*rpc.BlockHeader, error) {
	if h, ok := c.values[number]; ok {
		return h, nil
	}
	h, err := rpcClient.GetBlockByNumber(ctx, number)
	if err != nil {
		return nil, err
	}
	c.values[number] = h
	return h, nil
}

func buildFilter(from, to uint64, decoder *decode.Registry) rpc.LogFilter {
	return rpc.LogFilter{
		FromBlock: hexutil.EncodeUint64(from),
		ToBlock:   hexutil.EncodeUint64(to),
		Address:   decoder.FilterAddresses(),
		Topics:    [][]string{decoder.FilterTopics()},
	}
}

func processRangeAdaptive(ctx context.Context, deps Deps, chunker *Chunker, from, to uint64) error {
	cache := NewBlockCache()
	current := from
	for current <= to {
		size := chunker.Current()
		remaining := to - current + 1
		if size > remaining {
			size = remaining
		}
		end := current + size - 1
		filter := buildFilter(current, end, deps.Decoder)
		logs, err := deps.RPC.GetLogs(ctx, filter)
		if err != nil {
			deps.Metrics.AddRPCErrors(1)
			if err == rpc.ErrTooManyResults {
				chunker.OnError()
				deps.Logger.Warn("too many results, shrinking chunk", slog.Uint64("chunk", chunker.Current()))
				continue
			}
			return fmt.Errorf("getLogs %d-%d: %w", current, end, err)
		}

		records, err := buildRecords(ctx, deps, cache, logs)
		if err != nil {
			return err
		}
		if err := deps.Store.InsertLogs(ctx, records); err != nil {
			return fmt.Errorf("insert logs: %w", err)
		}
		header, err := deps.RPC.GetBlockByNumber(ctx, end)
		if err != nil {
			return fmt.Errorf("fetch block hash: %w", err)
		}
		cp := store.Checkpoint{
			StartBlock: current,
			EndBlock:   end,
			BlockHash:  header.Hash,
			InsertedAt: time.Now().UTC(),
		}
		if err := deps.Store.SaveRangeCheckpoint(ctx, cp); err != nil {
			return fmt.Errorf("save checkpoint: %w", err)
		}
		deps.Metrics.AddBlocks(size)
		deps.Metrics.AddLogs(uint64(len(records)))
		chunker.OnSuccess()
		current = end + 1
	}
	return nil
}

func buildRecords(ctx context.Context, deps Deps, cache *BlockCache, logs []types.Log) ([]store.LogRecord, error) {
	if len(logs) == 0 {
		return nil, nil
	}
	records := make([]store.LogRecord, 0, len(logs))
	for _, l := range logs {
		decoded, err := deps.Decoder.DecodeLog(l)
		if err != nil {
			return nil, fmt.Errorf("decode log: %w", err)
		}
		argsJSON, err := decoded.ArgsJSON()
		if err != nil {
			return nil, fmt.Errorf("args json: %w", err)
		}
		topicsJSON, err := json.Marshal(decode.TopicsToHex(l.Topics))
		if err != nil {
			return nil, fmt.Errorf("topics json: %w", err)
		}
		header, err := cache.Get(ctx, deps.RPC, l.BlockNumber)
		if err != nil {
			return nil, fmt.Errorf("block header: %w", err)
		}
		records = append(records, store.LogRecord{
			ChainID:         deps.ChainID,
			ContractAddress: l.Address.Hex(),
			EventName:       decoded.EventName,
			BlockNumber:     l.BlockNumber,
			BlockHash:       l.BlockHash.Hex(),
			TxHash:          l.TxHash.Hex(),
			LogIndex:        l.Index,
			Timestamp:       header.Timestamp,
			ArgsJSON:        argsJSON,
			TopicsJSON:      string(topicsJSON),
			DataHex:         hexutil.Encode(l.Data),
			InsertedAt:      time.Now().UTC(),
		})
	}
	return records, nil
}
