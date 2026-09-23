package keeper

import (
	"context"
	"os"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtcrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
	ics23 "github.com/cosmos/ics23/go"
	"github.com/stretchr/testify/require"
)

type stubQueryer struct {
	resp *abci.ResponseQuery
	err  error
}

func (s stubQueryer) Query(_ context.Context, _ *abci.RequestQuery) (*abci.ResponseQuery, error) {
	return s.resp, s.err
}

func newColdDataKeeper(t *testing.T, q ABCIQueryer) (sdk.Context, Keeper) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "reg-w1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })

	ctx, k := CreateTestInput(t, false, tempDir, true)
	k.queryer = q
	k.coldDataSet = false
	return ctx, k
}

func TestMaybeSetEnclaveColdData_NilProofOpsSkips(t *testing.T) {
	ctx, k := newColdDataKeeper(t, stubQueryer{resp: &abci.ResponseQuery{ProofOps: nil}})
	require.NoError(t, k.AddMachineSwapInfo(ctx, []byte("machine-id")))
	err := k.MaybeSetEnclaveColdData(ctx)
	require.NoError(t, err)
}

func TestMaybeSetEnclaveColdData_NilLeafSkips(t *testing.T) {
	cp := ics23.CommitmentProof{
		Proof: &ics23.CommitmentProof_Exist{
			Exist: &ics23.ExistenceProof{
				Key:   []byte("k"),
				Value: []byte("v"),
				Leaf:  nil,
			},
		},
	}
	bz, err := proto.Marshal(&cp)
	require.NoError(t, err)

	ctx, k := newColdDataKeeper(t, stubQueryer{
		resp: &abci.ResponseQuery{
			ProofOps: &cmtcrypto.ProofOps{
				Ops: []cmtcrypto.ProofOp{{Type: "ics23:iavl", Data: bz}},
			},
		},
	})
	require.NoError(t, k.AddMachineSwapInfo(ctx, []byte("machine-id")))
	err = k.MaybeSetEnclaveColdData(ctx)
	require.NoError(t, err)
}

func TestSerializeMerkleProof_NilLeaf(t *testing.T) {
	cp := ics23.CommitmentProof{
		Proof: &ics23.CommitmentProof_Exist{
			Exist: &ics23.ExistenceProof{Leaf: nil},
		},
	}
	bz, err := proto.Marshal(&cp)
	require.NoError(t, err)
	err, _ = SerializeMerkleProof([]cmtcrypto.ProofOp{{Type: "ics23:iavl", Data: bz}})
	require.Error(t, err)
}
