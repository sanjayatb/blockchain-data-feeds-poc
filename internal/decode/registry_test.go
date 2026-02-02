package decode

import (
	"math/big"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestDecodeERC20Transfer(t *testing.T) {
	abiPath := filepath.Join("..", "..", "abis", "erc20.json")
	reg, err := LoadRegistry([]ContractSpec{{
		Name:    "ERC20",
		Address: "0x1111111111111111111111111111111111111111",
		ABIPath: abiPath,
		Events:  []string{"Transfer"},
	}})
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}

	from := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	to := common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	value := big.NewInt(1000)

	// topic0 = keccak256("Transfer(address,address,uint256)")
	topic0 := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	topics := []common.Hash{
		topic0,
		common.BytesToHash(from.Bytes()),
		common.BytesToHash(to.Bytes()),
	}
	data := common.LeftPadBytes(value.Bytes(), 32)
	log := types.Log{
		Address:     common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Topics:      topics,
		Data:        data,
		BlockNumber: 123,
	}

	decoded, err := reg.DecodeLog(log)
	if err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if decoded.EventName != "Transfer" {
		t.Fatalf("expected Transfer got %s", decoded.EventName)
	}
	if decoded.Args["from"].(common.Address) != from {
		t.Fatalf("from mismatch")
	}
	if decoded.Args["to"].(common.Address) != to {
		t.Fatalf("to mismatch")
	}
	if decoded.Args["value"].(*big.Int).Cmp(value) != 0 {
		t.Fatalf("value mismatch")
	}
}
