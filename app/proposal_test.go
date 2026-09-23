package app

import (
	"errors"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func decodeOK(bz []byte) (sdk.Tx, error) {
	if len(bz) == 0 || bz[0] == 0xff {
		return nil, errors.New("malformed")
	}
	return nil, nil
}

func TestPrepareProposalDropsMalformedAndDoesNotReturnErr(t *testing.T) {
	h := DropMalformedPrepareProposal(decodeOK, nil)
	resp, err := h(sdk.Context{}, &abci.RequestPrepareProposal{
		Txs:        [][]byte{{0x01, 0x02}, {0xff, 0x00}, {0x03}},
		MaxTxBytes: 1024,
	})
	require.NoError(t, err)
	require.Equal(t, [][]byte{{0x01, 0x02}, {0x03}}, resp.Txs)
}

func TestPrepareProposalEmptyAfterDrop(t *testing.T) {
	h := DropMalformedPrepareProposal(decodeOK, nil)
	resp, err := h(sdk.Context{}, &abci.RequestPrepareProposal{Txs: [][]byte{{0xff}}})
	require.NoError(t, err)
	require.Empty(t, resp.Txs)
}

func TestPrepareProposalNextErrDoesNotReturnErr(t *testing.T) {
	next := func(_ sdk.Context, _ *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
		return nil, errors.New("swallowed")
	}
	h := DropMalformedPrepareProposal(decodeOK, next)
	resp, err := h(sdk.Context{}, &abci.RequestPrepareProposal{
		Txs: [][]byte{{0x01}, {0xff}},
	})
	require.NoError(t, err)
	require.Equal(t, [][]byte{{0x01}}, resp.Txs)
}

func TestProcessProposalRejectsMalformed(t *testing.T) {
	h := RejectMalformedProcessProposal(decodeOK, nil)
	resp, err := h(sdk.Context{}, &abci.RequestProcessProposal{Txs: [][]byte{{0x01}, {0xff}}})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)
}

func TestProcessProposalAcceptsWellFormed(t *testing.T) {
	h := RejectMalformedProcessProposal(decodeOK, nil)
	resp, err := h(sdk.Context{}, &abci.RequestProcessProposal{Txs: [][]byte{{0x01}, {0x02}}})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status)
}
