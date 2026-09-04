package meme

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Robinhood Chain Uniswap deployments (verified via eth_getCode).
var (
	WETH            = common.HexToAddress("0x0Bd7D308f8E1639FAb988df18A8011f41EAcAD73")
	V2Factory       = common.HexToAddress("0x8bcEaA40B9AcdfAedF85AdF4FF01F5Ad6517937f")
	V3Factory       = common.HexToAddress("0x1f7d7550B1b028f7571E69A784071F0205FD2EfA")
	V3PositionMgr   = common.HexToAddress("0x73991a25C818Bf1f1128dEAaB1492D45638DE0D3")
	V4PoolManager   = common.HexToAddress("0x8366a39CC670B4001A1121B8F6A443A643e40951")
	DeadAddress     = common.HexToAddress("0x000000000000000000000000000000000000dead")
	ZeroAddress     = common.Address{}
)

var QuoteTokens = map[common.Address]string{
	WETH: "WETH",
}

var (
	PairCreatedTopic = crypto.Keccak256Hash([]byte("PairCreated(address,address,address,uint256)"))
	PoolCreatedTopic = crypto.Keccak256Hash([]byte("PoolCreated(address,address,uint24,int24,address)"))
	V2SwapTopic      = crypto.Keccak256Hash([]byte("Swap(address,uint256,uint256,uint256,uint256,address)"))
	V3SwapTopic      = crypto.Keccak256Hash([]byte("Swap(address,address,int256,int256,uint160,uint128,int24)"))
	V2MintTopic      = crypto.Keccak256Hash([]byte("Mint(address,uint256,uint256)"))
	V2BurnTopic      = crypto.Keccak256Hash([]byte("Burn(address,uint256,uint256,address)"))
	TransferTopic    = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	// Uniswap v4 PoolManager Initialize
	V4InitializeTopic = crypto.Keccak256Hash([]byte("Initialize(bytes32,address,address,uint24,int24,address,uint160,int24)"))
)

// Known LP sink / locker addresses. Burn addresses always count; lockers are best-effort
// and can be extended as RH-specific lockers appear.
var LPLockAddresses = []common.Address{
	DeadAddress,
	ZeroAddress,
}

const (
	MaxTokenAge        = 30 * 24 * 3600 // seconds helper; prefer time.Duration in callers
	LPLockThresholdPct = 95.0
	AlertMinScore      = 70.0
)
