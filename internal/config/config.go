package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Network   string                 `yaml:"network"`
	Networks  map[string]ChainConfig `yaml:"networks"`
	Chain     ChainConfig            `yaml:"chain"`
	Indexer   IndexerConfig          `yaml:"indexer"`
	Database  DatabaseConfig         `yaml:"database"`
	Mempool   MempoolConfig          `yaml:"mempool"`
	UI        UIConfig               `yaml:"ui"`
	Contracts []ContractConfig       `yaml:"contracts"`
}

type ChainConfig struct {
	RPCURL  string `yaml:"rpc_url"`
	ChainID uint64 `yaml:"chain_id"`
}

type IndexerConfig struct {
	StartBlock             uint64  `yaml:"start_block"`
	Confirmations          uint64  `yaml:"confirmations"`
	ChunkSize              uint64  `yaml:"chunk_size"`
	MinChunkSize           uint64  `yaml:"min_chunk_size"`
	MaxChunkSize           uint64  `yaml:"max_chunk_size"`
	Concurrency            int     `yaml:"concurrency"`
	RateLimitPerSecond     float64 `yaml:"rate_limit_per_second"`
	PollIntervalSeconds    int     `yaml:"poll_interval_seconds"`
	MetricsIntervalSeconds int     `yaml:"metrics_interval_seconds"`
}

type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

type ContractConfig struct {
	Name    string   `yaml:"name"`
	Address string   `yaml:"address"`
	ABIPath string   `yaml:"abi_path"`
	Events  []string `yaml:"events"`
}

type MempoolConfig struct {
	WSURL             string        `yaml:"ws_url"`
	HTTPURL           string        `yaml:"http_url"`
	PendingWorkers    int           `yaml:"pending_workers"`
	ReceiptWorkers    int           `yaml:"receipt_workers"`
	DroppedTTL        time.Duration `yaml:"dropped_ttl"`
	DecodeInput       bool          `yaml:"decode_input"`
	TrackLifecycle    bool          `yaml:"track_lifecycle"`
	MaxInflightHashes int           `yaml:"max_inflight_hashes"`
}

type UIConfig struct {
	ListenAddr          string `yaml:"listen_addr"`
	DefaultFeed         string `yaml:"default_feed"`
	PollIntervalSeconds int    `yaml:"poll_interval_seconds"`
	DefaultFrom         string `yaml:"default_from"`
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	if err := c.resolveNetwork(); err != nil {
		return err
	}
	if c.Indexer.ChunkSize == 0 {
		return fmt.Errorf("indexer.chunk_size is required")
	}
	if c.Indexer.MinChunkSize == 0 {
		c.Indexer.MinChunkSize = 10
	}
	if c.Indexer.MaxChunkSize == 0 {
		c.Indexer.MaxChunkSize = c.Indexer.ChunkSize
	}
	if c.Indexer.MaxChunkSize < c.Indexer.MinChunkSize {
		c.Indexer.MaxChunkSize = c.Indexer.MinChunkSize
	}
	if c.Indexer.ChunkSize < c.Indexer.MinChunkSize {
		c.Indexer.ChunkSize = c.Indexer.MinChunkSize
	}
	if c.Indexer.ChunkSize > c.Indexer.MaxChunkSize {
		c.Indexer.ChunkSize = c.Indexer.MaxChunkSize
	}
	if c.Indexer.Concurrency <= 0 {
		c.Indexer.Concurrency = 4
	}
	if c.Indexer.PollIntervalSeconds <= 0 {
		c.Indexer.PollIntervalSeconds = 10
	}
	if c.Indexer.MetricsIntervalSeconds <= 0 {
		c.Indexer.MetricsIntervalSeconds = 30
	}
	if c.Database.Driver == "" {
		c.Database.Driver = "sqlite"
	}
	if c.Database.DSN == "" {
		return fmt.Errorf("database.dsn is required")
	}
	c.applyMempoolDefaults()
	c.applyUIDefaults()
	if len(c.Contracts) == 0 {
		return fmt.Errorf("contracts list is required")
	}
	for i, ctr := range c.Contracts {
		if ctr.Address == "" {
			return fmt.Errorf("contracts[%d].address is required", i)
		}
		if ctr.ABIPath == "" {
			return fmt.Errorf("contracts[%d].abi_path is required", i)
		}
		if len(ctr.Events) == 0 {
			return fmt.Errorf("contracts[%d].events is required", i)
		}
	}
	return nil
}

func (c *Config) resolveNetwork() error {
	if c.Network != "" {
		if len(c.Networks) == 0 {
			return fmt.Errorf("networks map is required when network is set")
		}
		chain, ok := c.Networks[c.Network]
		if !ok {
			return fmt.Errorf("network %q not found in networks", c.Network)
		}
		c.Chain = chain
	}
	if c.Chain.RPCURL == "" {
		return fmt.Errorf("chain.rpc_url is required")
	}
	if c.Chain.ChainID == 0 {
		return fmt.Errorf("chain.chain_id is required")
	}
	return nil
}

func (c *Config) applyMempoolDefaults() {
	if c.Mempool.HTTPURL == "" {
		c.Mempool.HTTPURL = c.Chain.RPCURL
	}
	if c.Mempool.WSURL == "" && c.Chain.RPCURL != "" {
		c.Mempool.WSURL = deriveWSURL(c.Chain.RPCURL)
	}
	if c.Mempool.PendingWorkers <= 0 {
		c.Mempool.PendingWorkers = 32
	}
	if c.Mempool.ReceiptWorkers <= 0 {
		c.Mempool.ReceiptWorkers = 16
	}
	if c.Mempool.DroppedTTL <= 0 {
		c.Mempool.DroppedTTL = 10 * time.Minute
	}
	if c.Mempool.MaxInflightHashes <= 0 {
		c.Mempool.MaxInflightHashes = 50000
	}
}

func deriveWSURL(httpURL string) string {
	if strings.HasPrefix(httpURL, "https://") {
		return "wss://" + strings.TrimPrefix(httpURL, "https://")
	}
	if strings.HasPrefix(httpURL, "http://") {
		return "ws://" + strings.TrimPrefix(httpURL, "http://")
	}
	return httpURL
}

func (c *Config) applyUIDefaults() {
	if c.UI.ListenAddr == "" {
		c.UI.ListenAddr = ":8080"
	}
	if c.UI.DefaultFeed == "" {
		c.UI.DefaultFeed = "mempool"
	}
	if c.UI.PollIntervalSeconds <= 0 {
		c.UI.PollIntervalSeconds = 2
	}
}
