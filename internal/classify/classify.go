package classify

import (
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	TransferTopic        = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	TransferSingleTopic  = common.HexToHash("0xc3d58168c5ae7397731d063d5bbf3d657854427343f4c083240f7aacaa2d0f62")
	TransferBatchTopic   = common.HexToHash("0x4a39dc06d4c0dbc64b70af90fd698a233a518aa5d07e595d983b8c0526c8f7fb")
	ZeroAddress          = common.Address{}
)

type Action string

const (
	ActionMint     Action = "mint"
	ActionBuy      Action = "buy"
	ActionSell     Action = "sell"
	ActionTransfer Action = "transfer"
)

type Event struct {
	TxHash      common.Hash
	LogIndex    uint
	BlockNumber uint64
	Collection  common.Address
	From        common.Address
	To          common.Address
	TokenID     *big.Int
	Action      Action
	Standard    string
}

func ParseLog(lg types.Log) (*Event, bool) {
	if len(lg.Topics) == 0 {
		return nil, false
	}
	switch lg.Topics[0] {
	case TransferTopic:
		if len(lg.Topics) < 4 {
			return nil, false
		}
		from := common.BytesToAddress(lg.Topics[1].Bytes())
		to := common.BytesToAddress(lg.Topics[2].Bytes())
		tokenID := new(big.Int).SetBytes(lg.Topics[3].Bytes())
		return &Event{
			TxHash:      lg.TxHash,
			LogIndex:    lg.Index,
			BlockNumber: lg.BlockNumber,
			Collection:  lg.Address,
			From:        from,
			To:          to,
			TokenID:     tokenID,
			Action:      classifyAction(from, to),
			Standard:    "ERC-721",
		}, true
	case TransferSingleTopic:
		if len(lg.Topics) < 4 || len(lg.Data) < 64 {
			return nil, false
		}
		from := common.BytesToAddress(lg.Topics[2].Bytes())
		to := common.BytesToAddress(lg.Topics[3].Bytes())
		tokenID := new(big.Int).SetBytes(lg.Data[:32])
		return &Event{
			TxHash:      lg.TxHash,
			LogIndex:    lg.Index,
			BlockNumber: lg.BlockNumber,
			Collection:  lg.Address,
			From:        from,
			To:          to,
			TokenID:     tokenID,
			Action:      classifyAction(from, to),
			Standard:    "ERC-1155",
		}, true
	default:
		return nil, false
	}
}

func classifyAction(from, to common.Address) Action {
	if from == ZeroAddress {
		return ActionMint
	}
	if to == ZeroAddress {
		return ActionTransfer
	}
	return ActionTransfer
}

func MatchWatch(ev *Event, watch map[string]struct{}, alertOnSell bool) (matched string, action Action, ok bool) {
	to := strings.ToLower(ev.To.Hex())
	from := strings.ToLower(ev.From.Hex())
	if _, hit := watch[to]; hit {
		if ev.From == ZeroAddress {
			return to, ActionMint, true
		}
		return to, ActionBuy, true
	}
	if alertOnSell {
		if _, hit := watch[from]; hit && ev.To != ZeroAddress {
			return from, ActionSell, true
		}
	}
	return "", "", false
}
