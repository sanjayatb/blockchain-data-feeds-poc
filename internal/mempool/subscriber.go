package mempool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"time"

	"github.com/gorilla/websocket"
)

type Subscriber struct {
	wsURL    string
	logger   *slog.Logger
	pendingC chan string
	headC    chan Head
}

type Subscription struct {
	ID string
}

type subscriptionParams struct {
	Subscription string          `json:"subscription"`
	Result       json.RawMessage `json:"result"`
}

type subscriptionMsg struct {
	JSONRPC string             `json:"jsonrpc"`
	Method  string             `json:"method"`
	Params  subscriptionParams `json:"params"`
}

type newHeadResult struct {
	Number string `json:"number"`
}

func NewSubscriber(wsURL string, logger *slog.Logger, pendingC chan string, headC chan Head) *Subscriber {
	return &Subscriber{wsURL: wsURL, logger: logger, pendingC: pendingC, headC: headC}
}

func (s *Subscriber) Run(ctx context.Context) {
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := s.runOnce(ctx); err != nil {
			s.logger.Warn("mempool subscriber disconnected", slog.String("error", err.Error()))
			s.sleepBackoff(&backoff)
			continue
		}
		backoff = time.Second
	}
}

func (s *Subscriber) runOnce(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, s.wsURL, nil)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	defer conn.Close()

	pendingSub, err := s.subscribe(conn, "newPendingTransactions")
	if err != nil {
		return err
	}
	headSub, err := s.subscribe(conn, "newHeads")
	if err != nil {
		return err
	}
	s.logger.Info("mempool subscribed", slog.String("pending_sub", pendingSub.ID), slog.String("head_sub", headSub.ID))

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var subMsg subscriptionMsg
		if err := json.Unmarshal(msg, &subMsg); err != nil {
			s.logger.Debug("invalid subscription message", slog.String("error", err.Error()))
			continue
		}
		if subMsg.Method != "eth_subscription" {
			continue
		}
		s.dispatch(subMsg)
	}
}

func (s *Subscriber) subscribe(conn *websocket.Conn, channel string) (*Subscription, error) {
	id := rand.Intn(1_000_000)
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "eth_subscribe",
		"params":  []any{channel},
	}
	if err := conn.WriteJSON(req); err != nil {
		return nil, fmt.Errorf("subscribe %s: %w", channel, err)
	}
	_, msg, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result string          `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(msg, &resp); err != nil {
		return nil, fmt.Errorf("subscribe parse: %w", err)
	}
	if resp.Result == "" {
		return nil, fmt.Errorf("subscribe %s failed", channel)
	}
	return &Subscription{ID: resp.Result}, nil
}

func (s *Subscriber) dispatch(msg subscriptionMsg) {
	if msg.Params.Subscription == "" {
		return
	}
	if len(msg.Params.Result) == 0 {
		return
	}
	if msg.Params.Result[0] == '"' {
		var hash string
		if err := json.Unmarshal(msg.Params.Result, &hash); err == nil {
			select {
			case s.pendingC <- hash:
			default:
				s.logger.Warn("pending channel full, dropping hash")
			}
		}
		return
	}
	var head newHeadResult
	if err := json.Unmarshal(msg.Params.Result, &head); err != nil {
		return
	}
	if head.Number == "" {
		return
	}
	num, err := parseHexUint(head.Number)
	if err != nil {
		return
	}
	select {
	case s.headC <- Head{Number: num}:
	default:
		s.logger.Debug("head channel full, dropping head")
	}
}

func (s *Subscriber) sleepBackoff(backoff *time.Duration) {
	wait := float64(*backoff) * (0.9 + rand.Float64()*0.2)
	if wait > float64(30*time.Second) {
		wait = float64(30 * time.Second)
	}
	s.logger.Debug("mempool reconnect backoff", slog.Duration("wait", time.Duration(wait)))
	time.Sleep(time.Duration(wait))
	*backoff = time.Duration(math.Min(float64(*backoff*2), float64(30*time.Second)))
}

func parseHexUint(v string) (uint64, error) {
	if len(v) > 1 && v[:2] == "0x" {
		return parseHexUint(v[2:])
	}
	var out uint64
	for _, r := range v {
		out <<= 4
		switch {
		case r >= '0' && r <= '9':
			out += uint64(r - '0')
		case r >= 'a' && r <= 'f':
			out += uint64(r-'a') + 10
		case r >= 'A' && r <= 'F':
			out += uint64(r-'A') + 10
		default:
			return 0, fmt.Errorf("invalid hex")
		}
	}
	return out, nil
}
