package decode

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

type InputDecoded struct {
	Method string
	Args   map[string]any
}

func (r *Registry) DecodeInput(address string, inputHex string) (*InputDecoded, error) {
	if inputHex == "" || inputHex == "0x" {
		return nil, nil
	}
	addr := strings.ToLower(common.HexToAddress(address).Hex())
	parsed, ok := r.abiByAddress[addr]
	if !ok {
		return nil, fmt.Errorf("no ABI for address %s", address)
	}
	inputHex = strings.TrimPrefix(inputHex, "0x")
	if len(inputHex) < 8 {
		return nil, fmt.Errorf("input too short")
	}
	data, err := hex.DecodeString(inputHex)
	if err != nil {
		return nil, fmt.Errorf("decode input: %w", err)
	}
	method, err := parsed.MethodById(data[:4])
	if err != nil {
		return nil, err
	}
	args := map[string]any{}
	if err := method.Inputs.UnpackIntoMap(args, data[4:]); err != nil {
		return nil, err
	}
	return &InputDecoded{Method: method.Name, Args: args}, nil
}

func FormatDecodedInput(decoded *InputDecoded) map[string]any {
	if decoded == nil {
		return nil
	}
	return map[string]any{
		"method": decoded.Method,
		"args":   decoded.Args,
	}
}
