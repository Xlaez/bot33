package chain

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	ChainID    int64  = 4663
	Name       string = "Robinhood Chain"
	Explorer   string = "https://robinhoodchain.blockscout.com"
	NativeSym  string = "ETH"
	DefaultRPC string = "https://rpc.mainnet.chain.robinhood.com"
)

var PublicRPCs = []string{
	"https://rpc.mainnet.chain.robinhood.com",
}

type Client struct {
	HTTP *ethclient.Client
	WS   *ethclient.Client
}

func Dial(ctx context.Context, httpURL, wsURL string, expectedChainID int64) (*Client, error) {
	httpClient, err := ethclient.DialContext(ctx, httpURL)
	if err != nil {
		return nil, fmt.Errorf("dial http rpc: %w", err)
	}
	id, err := httpClient.ChainID(ctx)
	if err != nil {
		httpClient.Close()
		return nil, fmt.Errorf("chain id: %w", err)
	}
	if id.Cmp(big.NewInt(expectedChainID)) != 0 {
		httpClient.Close()
		return nil, fmt.Errorf("unexpected chain id %s want %d", id.String(), expectedChainID)
	}
	c := &Client{HTTP: httpClient}
	if wsURL != "" {
		wsClient, err := ethclient.DialContext(ctx, wsURL)
		if err != nil {
			httpClient.Close()
			return nil, fmt.Errorf("dial ws rpc: %w", err)
		}
		c.WS = wsClient
	}
	return c, nil
}

func (c *Client) Close() {
	if c.HTTP != nil {
		c.HTTP.Close()
	}
	if c.WS != nil {
		c.WS.Close()
	}
}

func ExplorerTx(hash string) string {
	return Explorer + "/tx/" + hash
}

func ExplorerAddress(addr string) string {
	return Explorer + "/address/" + addr
}
