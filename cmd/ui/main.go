package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"blockchain-data-feeds-poc/internal/config"
	"blockchain-data-feeds-poc/internal/rpc"
	"blockchain-data-feeds-poc/internal/store"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"gopkg.in/yaml.v3"
)

//go:embed static/*
var staticFS embed.FS

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := mustLoadConfig()
	st, err := store.New(cfg.Database)
	if err != nil {
		panic(err)
	}
	defer st.Close()

	rpcClient := rpc.NewClient(cfg.Mempool.HTTPURL, cfg.Mempool.PendingWorkers, cfg.Indexer.RateLimitPerSecond, logger)
	server := newServer(cfg, st, rpcClient)

	addr := cfg.UI.ListenAddr
	if addr == "" {
		addr = ":8080"
	}

	fmt.Printf("UI listening on %s\n", addr)
	if err := http.ListenAndServe(addr, server.routes()); err != nil {
		panic(err)
	}
}

type server struct {
	cfg *config.Config
	st  store.Store
	rpc *rpc.Client
}

func newServer(cfg *config.Config, st store.Store, rpcClient *rpc.Client) *server {
	return &server{cfg: cfg, st: st, rpc: rpcClient}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(staticFS, "static")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/mempool", s.handleMempool)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/stream", s.handleStream)
	mux.HandleFunc("/api/accounts", s.handleAccounts)
	mux.HandleFunc("/api/tx/send", s.handleSendTx)
	mux.HandleFunc("/api/mine", s.handleMine)
	mux.HandleFunc("/api/token/latest", s.handleTokenLatest)
	mux.HandleFunc("/api/token/apply", s.handleTokenApply)
	return mux
}

func (s *server) handleMempool(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	limit := parseInt(r.URL.Query().Get("limit"), 100)
	rows, err := s.st.ListMempool(r.Context(), status, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, rows)
}

func (s *server) handleLogs(w http.ResponseWriter, r *http.Request) {
	limit := parseInt(r.URL.Query().Get("limit"), 100)
	rows, err := s.st.ListLogs(r.Context(), limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, rows)
}

func (s *server) handleStream(w http.ResponseWriter, r *http.Request) {
	feed := r.URL.Query().Get("feed")
	status := r.URL.Query().Get("status")
	limit := parseInt(r.URL.Query().Get("limit"), 100)
	if feed == "" {
		feed = s.cfg.UI.DefaultFeed
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, fmt.Errorf("streaming not supported"))
		return
	}

	ticker := time.NewTicker(time.Duration(s.cfg.UI.PollIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			var payload any
			var err error
			switch feed {
			case "logs":
				payload, err = s.st.ListLogs(r.Context(), limit)
			default:
				payload, err = s.st.ListMempool(r.Context(), status, limit)
			}
			if err != nil {
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", escapeJSON(err.Error()))
				flusher.Flush()
				continue
			}
			b, _ := json.Marshal(payload)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
	}
}

func (s *server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	var accounts []string
	if err := s.rpc.Call(r.Context(), "eth_accounts", []any{}, &accounts); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, accounts)
}

func (s *server) handleSendTx(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		From     string  `json:"from"`
		To       string  `json:"to"`
		ValueWei string  `json:"value_wei"`
		GasLimit uint64  `json:"gas_limit"`
		GasPrice string  `json:"gas_price_wei"`
		Nonce    *uint64 `json:"nonce"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err)
		return
	}
	if req.From == "" {
		req.From = s.cfg.UI.DefaultFrom
	}
	if req.From == "" || req.To == "" {
		writeError(w, fmt.Errorf("from and to are required"))
		return
	}
	if !common.IsHexAddress(req.From) || !common.IsHexAddress(req.To) {
		writeError(w, fmt.Errorf("invalid from/to address"))
		return
	}

	params := map[string]string{
		"from": strings.ToLower(req.From),
		"to":   strings.ToLower(req.To),
	}
	if req.ValueWei != "" {
		val, ok := new(big.Int).SetString(req.ValueWei, 10)
		if !ok {
			writeError(w, fmt.Errorf("invalid value_wei"))
			return
		}
		params["value"] = hexutil.EncodeBig(val)
	}
	if req.GasLimit > 0 {
		params["gas"] = hexutil.EncodeUint64(req.GasLimit)
	}
	if req.GasPrice != "" {
		val, ok := new(big.Int).SetString(req.GasPrice, 10)
		if !ok {
			writeError(w, fmt.Errorf("invalid gas_price_wei"))
			return
		}
		params["gasPrice"] = hexutil.EncodeBig(val)
	}
	if req.Nonce != nil {
		params["nonce"] = hexutil.EncodeUint64(*req.Nonce)
	}

	var hash string
	if err := s.rpc.Call(r.Context(), "eth_sendTransaction", []any{params}, &hash); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]string{"tx_hash": hash})
}

func (s *server) handleMine(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var res string
	if err := s.rpc.Call(r.Context(), "evm_mine", []any{}, &res); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]string{"result": "ok"})
}

func (s *server) handleTokenLatest(w http.ResponseWriter, r *http.Request) {
	path := "/app/data/token.json"
	if _, err := os.Stat(path); err != nil {
		writeError(w, fmt.Errorf("token.json not found"))
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

func (s *server) handleTokenApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err)
		return
	}
	addr := strings.TrimSpace(req.Address)
	if addr == "" {
		writeError(w, fmt.Errorf("address is required"))
		return
	}
	if err := updateConfigTokenAddress(os.Getenv("CONFIG_PATH"), addr); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]string{"result": "ok"})
}

func mustLoadConfig() *config.Config {
	path := "configs/config.sample.yaml"
	if v := os.Getenv("CONFIG_PATH"); v != "" {
		path = v
	}
	cfg, err := config.Load(path)
	if err != nil {
		panic(err)
	}
	return cfg
}

func updateConfigTokenAddress(path string, address string) error {
	if path == "" {
		path = "configs/config.sample.yaml"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg config.Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return err
	}
	if len(cfg.Contracts) == 0 {
		return fmt.Errorf("no contracts in config")
	}
	idx := 0
	for i, c := range cfg.Contracts {
		if strings.EqualFold(c.Name, "LocalERC20") || strings.EqualFold(c.Name, "TestToken") {
			idx = i
			break
		}
	}
	cfg.Contracts[idx].Address = address
	out, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func parseInt(v string, def int) int {
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
