package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/time/rate"
	"log/slog"
)

var ErrTooManyResults = errors.New("too many results")

const (
	defaultRetries     = 5
	defaultBackoff     = 200 * time.Millisecond
	maxBackoff         = 5 * time.Second
	defaultHTTPTimeout = 30 * time.Second
)

type Client struct {
	url        string
	httpClient *http.Client
	limiter    *rate.Limiter
	sem        chan struct{}
	retries    int
	backoff    time.Duration
	logger     *slog.Logger
}

func NewClient(url string, concurrency int, rateLimit float64, logger *slog.Logger) *Client {
	if concurrency <= 0 {
		concurrency = 4
	}
	lim := rate.Inf
	if rateLimit > 0 {
		lim = rate.Limit(rateLimit)
	}
	return &Client{
		url:        url,
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
		limiter:    rate.NewLimiter(lim, concurrency),
		sem:        make(chan struct{}, concurrency),
		retries:    defaultRetries,
		backoff:    defaultBackoff,
		logger:     logger,
	}
}

func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	payload := rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	for attempt := 0; attempt <= c.retries; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return fmt.Errorf("rate limit wait: %w", err)
		}
		c.sem <- struct{}{}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
		if err != nil {
			<-c.sem
			return fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.httpClient.Do(req)
		<-c.sem
		if err != nil {
			if shouldRetry(err) && attempt < c.retries {
				c.sleepBackoff(attempt)
				continue
			}
			return fmt.Errorf("rpc post: %w", err)
		}
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("read response: %w", err)
		}
		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			if attempt < c.retries {
				c.sleepBackoff(attempt)
				continue
			}
			return fmt.Errorf("rpc http status %d", resp.StatusCode)
		}
		var rpcResp rpcResponse
		if err := json.Unmarshal(respBody, &rpcResp); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
		if rpcResp.Error != nil {
			if isTooManyResults(rpcResp.Error.Message) {
				return ErrTooManyResults
			}
			if attempt < c.retries && isRetryableRPCError(rpcResp.Error) {
				c.sleepBackoff(attempt)
				continue
			}
			return fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
		}
		if result == nil {
			return nil
		}
		if err := json.Unmarshal(rpcResp.Result, result); err != nil {
			return fmt.Errorf("unmarshal result: %w", err)
		}
		return nil
	}
	return fmt.Errorf("rpc call failed after retries")
}

func (c *Client) GetBlockNumber(ctx context.Context) (uint64, error) {
	var hexNum string
	if err := c.Call(ctx, "eth_blockNumber", []any{}, &hexNum); err != nil {
		return 0, err
	}
	return hexutil.DecodeUint64(hexNum)
}

func (c *Client) GetBlockByNumber(ctx context.Context, number uint64) (*BlockHeader, error) {
	var raw blockResponse
	if err := c.Call(ctx, "eth_getBlockByNumber", []any{hexutil.EncodeUint64(number), false}, &raw); err != nil {
		return nil, err
	}
	return raw.toHeader()
}

func (c *Client) GetBlockHashByNumber(ctx context.Context, number uint64) (string, error) {
	h, err := c.GetBlockByNumber(ctx, number)
	if err != nil {
		return "", err
	}
	return h.Hash, nil
}

func (c *Client) GetLogs(ctx context.Context, filter LogFilter) ([]types.Log, error) {
	var logs []types.Log
	if err := c.Call(ctx, "eth_getLogs", []any{filter}, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

func (c *Client) GetTransactionByHash(ctx context.Context, hash string) (*RPCTransaction, error) {
	var raw json.RawMessage
	if err := c.Call(ctx, "eth_getTransactionByHash", []any{hash}, &raw); err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var tx RPCTransaction
	if err := json.Unmarshal(raw, &tx); err != nil {
		return nil, err
	}
	return &tx, nil
}

func (c *Client) GetTransactionReceipt(ctx context.Context, hash string) (*RPCReceipt, error) {
	var raw json.RawMessage
	if err := c.Call(ctx, "eth_getTransactionReceipt", []any{hash}, &raw); err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var receipt RPCReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

func (c *Client) sleepBackoff(attempt int) {
	exp := float64(c.backoff) * math.Pow(2, float64(attempt))
	if exp > float64(maxBackoff) {
		exp = float64(maxBackoff)
	}
	jitter := rand.Float64()*0.2 + 0.9
	wait := time.Duration(exp * jitter)
	c.logger.Debug("rpc retry backoff", slog.Duration("wait", wait), slog.Int("attempt", attempt))
	time.Sleep(wait)
}

func shouldRetry(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
}

func isRetryableRPCError(err *rpcError) bool {
	if err == nil {
		return false
	}
	if err.Code == -32005 {
		return true
	}
	return strings.Contains(strings.ToLower(err.Message), "timeout")
}

func isTooManyResults(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "too many results") ||
		strings.Contains(m, "more than") ||
		strings.Contains(m, "query returned")
}

// JSON-RPC structures

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// log filter

type LogFilter struct {
	FromBlock string     `json:"fromBlock"`
	ToBlock   string     `json:"toBlock"`
	Address   []string   `json:"address,omitempty"`
	Topics    [][]string `json:"topics,omitempty"`
}

type blockResponse struct {
	Number    string `json:"number"`
	Hash      string `json:"hash"`
	Timestamp string `json:"timestamp"`
}

type RPCTransaction struct {
	Hash                 string          `json:"hash"`
	From                 string          `json:"from"`
	To                   *string         `json:"to"`
	Nonce                hexutil.Uint64  `json:"nonce"`
	Value                *hexutil.Big    `json:"value"`
	Gas                  hexutil.Uint64  `json:"gas"`
	GasPrice             *hexutil.Big    `json:"gasPrice"`
	MaxFeePerGas         *hexutil.Big    `json:"maxFeePerGas"`
	MaxPriorityFeePerGas *hexutil.Big    `json:"maxPriorityFeePerGas"`
	TxType               *hexutil.Uint64 `json:"type"`
	Input                string          `json:"input"`
}

type RPCReceipt struct {
	TransactionHash  string          `json:"transactionHash"`
	BlockHash        string          `json:"blockHash"`
	BlockNumber      *hexutil.Uint64 `json:"blockNumber"`
	TransactionIndex *hexutil.Uint64 `json:"transactionIndex"`
	Status           *hexutil.Uint64 `json:"status"`
}

type BlockHeader struct {
	Number    uint64
	Hash      string
	Timestamp time.Time
}

func (b blockResponse) toHeader() (*BlockHeader, error) {
	if b.Number == "" {
		return nil, fmt.Errorf("block not found")
	}
	num, err := hexutil.DecodeUint64(b.Number)
	if err != nil {
		return nil, fmt.Errorf("decode block number: %w", err)
	}
	ts, err := hexutil.DecodeUint64(b.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("decode timestamp: %w", err)
	}
	return &BlockHeader{
		Number:    num,
		Hash:      b.Hash,
		Timestamp: time.Unix(int64(ts), 0).UTC(),
	}, nil
}
