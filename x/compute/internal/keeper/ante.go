package keeper

import (
	"encoding/binary"

	"cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	"github.com/scrtlabs/SecretNetwork/x/compute/internal/types"
)

// CountTXDecorator ante handler to count the tx position in a block.
type CountTXDecorator struct {
	appcodec     codec.Codec
	govkeeper    govkeeper.Keeper
	storeService store.KVStoreService
	checker      PrivilegedChecker
}

// NewCountTXDecorator constructor
func NewCountTXDecorator(appcodec codec.Codec, govkeeper govkeeper.Keeper, storeService store.KVStoreService) *CountTXDecorator {
	return &CountTXDecorator{
		appcodec:     appcodec,
		govkeeper:    govkeeper,
		storeService: storeService,
		checker:      NewPrivilegedChecker(appcodec, govkeeper),
	}
}

// AnteHandle stores a tx counter and denylists nested privileged compute messages.
func (a CountTXDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if simulate {
		return next(ctx, tx, simulate)
	}
	store := a.storeService.OpenKVStore(ctx)
	currentHeight := ctx.BlockHeight()

	var txCounter uint32
	if bz, _ := store.Get(types.TXCounterPrefix); bz != nil {
		lastHeight, val := decodeHeightCounter(bz)
		if currentHeight == lastHeight {
			txCounter = val
		}
	}
	err := store.Set(types.TXCounterPrefix, encodeHeightCounter(currentHeight, txCounter+1))
	if err != nil {
		ctx.Logger().Error("compute ante store set", "store", err.Error())
	}

	if err = a.checker.CheckTxMsgs(ctx, tx.GetMsgs()); err != nil {
		return ctx, err
	}

	return next(types.WithTXCounter(ctx, txCounter), tx, simulate)
}

func encodeHeightCounter(height int64, counter uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, counter)
	return append(sdk.Uint64ToBigEndian(uint64(height)), b...)
}

func decodeHeightCounter(bz []byte) (int64, uint32) {
	return int64(sdk.BigEndianToUint64(bz[0:8])), binary.BigEndian.Uint32(bz[8:])
}
