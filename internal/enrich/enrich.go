package enrich

import (
	"context"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

var nameABI = mustABI(`[{"inputs":[],"name":"name","outputs":[{"type":"string"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"symbol","outputs":[{"type":"string"}],"stateMutability":"view","type":"function"}]`)

type Enricher struct {
	client *ethclient.Client
	mu     sync.RWMutex
	cache  map[string]string
}

func New(client *ethclient.Client) *Enricher {
	return &Enricher{client: client, cache: map[string]string{}}
}

func (e *Enricher) CollectionName(ctx context.Context, addr common.Address) string {
	key := strings.ToLower(addr.Hex())
	e.mu.RLock()
	if v, ok := e.cache[key]; ok {
		e.mu.RUnlock()
		return v
	}
	e.mu.RUnlock()

	name := e.callString(ctx, addr, "name")
	if name == "" {
		name = e.callString(ctx, addr, "symbol")
	}
	if name == "" {
		name = addr.Hex()
	}
	e.mu.Lock()
	e.cache[key] = name
	e.mu.Unlock()
	return name
}

func (e *Enricher) callString(ctx context.Context, addr common.Address, method string) string {
	data, err := nameABI.Pack(method)
	if err != nil {
		return ""
	}
	out, err := e.client.CallContract(ctx, ethereum.CallMsg{To: &addr, Data: data}, nil)
	if err != nil || len(out) == 0 {
		return ""
	}
	vals, err := nameABI.Unpack(method, out)
	if err != nil || len(vals) == 0 {
		return ""
	}
	s, _ := vals[0].(string)
	return strings.TrimSpace(s)
}

func mustABI(raw string) abi.ABI {
	a, err := abi.JSON(strings.NewReader(raw))
	if err != nil {
		panic(err)
	}
	return a
}
