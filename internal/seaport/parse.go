package seaport

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Address is OpenSea Seaport 1.6 (verified present on Robinhood Chain 4663).
var Address = common.HexToAddress("0x0000000000000068F116a894984e2DB1123eB395")

var OrderFulfilledTopic = crypto.Keccak256Hash([]byte(
	"OrderFulfilled(bytes32,address,address,address,(uint8,address,uint256,uint256)[],(uint8,address,uint256,uint256,address)[])",
))

const (
	itemNative  uint8 = 0
	itemERC20   uint8 = 1
	itemERC721  uint8 = 2
	itemERC1155 uint8 = 3
)

const eventABIJSON = `[
  {
    "anonymous": false,
    "inputs": [
      {"indexed": false, "name": "orderHash", "type": "bytes32"},
      {"indexed": true, "name": "offerer", "type": "address"},
      {"indexed": true, "name": "zone", "type": "address"},
      {"indexed": false, "name": "recipient", "type": "address"},
      {"indexed": false, "components": [
        {"name": "itemType", "type": "uint8"},
        {"name": "token", "type": "address"},
        {"name": "identifier", "type": "uint256"},
        {"name": "amount", "type": "uint256"}
      ], "name": "offer", "type": "tuple[]"},
      {"indexed": false, "components": [
        {"name": "itemType", "type": "uint8"},
        {"name": "token", "type": "address"},
        {"name": "identifier", "type": "uint256"},
        {"name": "amount", "type": "uint256"},
        {"name": "recipient", "type": "address"}
      ], "name": "consideration", "type": "tuple[]"}
    ],
    "name": "OrderFulfilled",
    "type": "event"
  }
]`

var eventABI = mustABI(eventABIJSON)

type Sale struct {
	TxHash      common.Hash
	LogIndex    uint
	BlockNumber uint64
	Buyer       common.Address
	Seller      common.Address
	Collection  common.Address
	TokenID     *big.Int
	PriceWei    *big.Int
}

type spentItem struct {
	ItemType   uint8
	Token      common.Address
	Identifier *big.Int
	Amount     *big.Int
}

type receivedItem struct {
	ItemType   uint8
	Token      common.Address
	Identifier *big.Int
	Amount     *big.Int
	Recipient  common.Address
}

type fulfilledPayload struct {
	OrderHash     [32]byte
	Recipient     common.Address
	Offer         []spentItem
	Consideration []receivedItem
}

func ParseOrderFulfilled(lg types.Log) (*Sale, bool, error) {
	if len(lg.Topics) < 3 || lg.Topics[0] != OrderFulfilledTopic {
		return nil, false, nil
	}
	offerer := common.BytesToAddress(lg.Topics[1].Bytes())

	var payload fulfilledPayload
	if err := eventABI.UnpackIntoInterface(&payload, "OrderFulfilled", lg.Data); err != nil {
		return nil, false, fmt.Errorf("unpack OrderFulfilled: %w", err)
	}

	nftOffer := firstNFTSpent(payload.Offer)
	nftRecv := firstNFTReceived(payload.Consideration)
	price := sumPayments(payload.Offer, payload.Consideration)

	sale := &Sale{
		TxHash:      lg.TxHash,
		LogIndex:    lg.Index,
		BlockNumber: lg.BlockNumber,
		PriceWei:    price,
		TokenID:     big.NewInt(0),
	}

	switch {
	case nftOffer != nil:
		sale.Seller = offerer
		sale.Buyer = payload.Recipient
		sale.Collection = nftOffer.Token
		sale.TokenID = nftOffer.Identifier
	case nftRecv != nil:
		sale.Buyer = offerer
		if nftRecv.Recipient != (common.Address{}) {
			sale.Buyer = nftRecv.Recipient
		}
		sale.Collection = nftRecv.Token
		sale.TokenID = nftRecv.Identifier
	default:
		return nil, false, nil
	}

	if sale.Buyer == (common.Address{}) && sale.Seller == (common.Address{}) {
		return nil, false, nil
	}
	if sale.PriceWei == nil {
		sale.PriceWei = big.NewInt(0)
	}
	return sale, true, nil
}

func firstNFTSpent(items []spentItem) *spentItem {
	for i := range items {
		if isNFT(items[i].ItemType) {
			return &items[i]
		}
	}
	return nil
}

func firstNFTReceived(items []receivedItem) *receivedItem {
	for i := range items {
		if isNFT(items[i].ItemType) {
			return &items[i]
		}
	}
	return nil
}

func isNFT(t uint8) bool {
	return t == itemERC721 || t == itemERC1155
}

func sumPayments(offer []spentItem, consideration []receivedItem) *big.Int {
	total := big.NewInt(0)
	for _, it := range offer {
		if it.ItemType == itemNative && it.Amount != nil {
			total.Add(total, it.Amount)
		}
	}
	for _, it := range consideration {
		if it.ItemType == itemNative && it.Amount != nil {
			total.Add(total, it.Amount)
		}
	}
	if total.Sign() == 0 {
		for _, it := range offer {
			if it.ItemType == itemERC20 && it.Amount != nil {
				total.Add(total, it.Amount)
			}
		}
		for _, it := range consideration {
			if it.ItemType == itemERC20 && it.Amount != nil {
				total.Add(total, it.Amount)
			}
		}
	}
	return total
}

func mustABI(raw string) abi.ABI {
	a, err := abi.JSON(strings.NewReader(raw))
	if err != nil {
		panic(err)
	}
	return a
}
