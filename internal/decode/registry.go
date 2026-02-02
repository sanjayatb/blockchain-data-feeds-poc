package decode

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type Registry struct {
	byTopicAddress map[string]EventSpec
	byTopic        map[common.Hash]EventSpec
	addresses      []common.Address
	topic0s        []common.Hash
	abiByAddress   map[string]abi.ABI
}

type EventSpec struct {
	ContractAddress common.Address
	EventName       string
	Event           abi.Event
}

type DecodedLog struct {
	EventName string
	Args      map[string]any
}

func LoadRegistry(contracts []ContractSpec) (*Registry, error) {
	reg := &Registry{
		byTopicAddress: make(map[string]EventSpec),
		byTopic:        make(map[common.Hash]EventSpec),
		abiByAddress:   make(map[string]abi.ABI),
	}
	for _, c := range contracts {
		addr := common.HexToAddress(c.Address)
		b, err := os.ReadFile(c.ABIPath)
		if err != nil {
			return nil, fmt.Errorf("read abi %s: %w", c.ABIPath, err)
		}
		parsed, err := abi.JSON(strings.NewReader(string(b)))
		if err != nil {
			return nil, fmt.Errorf("parse abi %s: %w", c.ABIPath, err)
		}
		reg.abiByAddress[strings.ToLower(addr.Hex())] = parsed
		for _, evtName := range c.Events {
			evt, ok := parsed.Events[evtName]
			if !ok {
				return nil, fmt.Errorf("event %s not found in %s", evtName, c.ABIPath)
			}
			spec := EventSpec{ContractAddress: addr, EventName: evt.Name, Event: evt}
			key := topicAddressKey(evt.ID, addr)
			reg.byTopicAddress[key] = spec
			reg.byTopic[evt.ID] = spec
			reg.topic0s = append(reg.topic0s, evt.ID)
		}
		reg.addresses = append(reg.addresses, addr)
	}
	return reg, nil
}

func (r *Registry) DecodeLog(log types.Log) (*DecodedLog, error) {
	if len(log.Topics) == 0 {
		return nil, fmt.Errorf("log has no topics")
	}
	topic0 := log.Topics[0]
	key := topicAddressKey(topic0, log.Address)
	spec, ok := r.byTopicAddress[key]
	if !ok {
		spec, ok = r.byTopic[topic0]
		if !ok {
			return nil, fmt.Errorf("unknown event topic %s", topic0.Hex())
		}
	}
	args := map[string]any{}
	indexed, nonIndexed := splitArgs(spec.Event.Inputs)
	if err := nonIndexed.UnpackIntoMap(args, log.Data); err != nil {
		return nil, fmt.Errorf("unpack data: %w", err)
	}
	if err := abi.ParseTopicsIntoMap(args, indexed, log.Topics[1:]); err != nil {
		return nil, fmt.Errorf("parse topics: %w", err)
	}
	return &DecodedLog{EventName: spec.EventName, Args: args}, nil
}

func splitArgs(args abi.Arguments) (indexed abi.Arguments, nonIndexed abi.Arguments) {
	for _, arg := range args {
		if arg.Indexed {
			indexed = append(indexed, arg)
		} else {
			nonIndexed = append(nonIndexed, arg)
		}
	}
	return indexed, nonIndexed
}

func topicAddressKey(topic common.Hash, addr common.Address) string {
	return strings.ToLower(topic.Hex() + "|" + addr.Hex())
}

// ContractSpec mirrors config but keeps it decoupled from YAML parsing.
type ContractSpec struct {
	Name    string
	Address string
	ABIPath string
	Events  []string
}

func (d *DecodedLog) ArgsJSON() (string, error) {
	b, err := json.Marshal(d.Args)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func TopicsToHex(topics []common.Hash) []string {
	out := make([]string, 0, len(topics))
	for _, t := range topics {
		out = append(out, t.Hex())
	}
	return out
}

func (r *Registry) FilterAddresses() []string {
	out := make([]string, 0, len(r.addresses))
	for _, a := range r.addresses {
		out = append(out, a.Hex())
	}
	return out
}

func (r *Registry) FilterTopics() []string {
	return TopicsToHex(r.topic0s)
}
