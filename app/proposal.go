package app

import (
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DropMalformedPrepareProposal drops txs that fail decode, then optionally
// calls the live default handler. Must not return err — SDK 0.50 swallows
// PrepareProposal err and echoes req.Txs. Does not add a mempool.
func DropMalformedPrepareProposal(decode sdk.TxDecoder, next sdk.PrepareProposalHandler) sdk.PrepareProposalHandler {
	return func(ctx sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
		kept := make([][]byte, 0, len(req.Txs))
		var size int64
		for _, txBz := range req.Txs {
			if _, err := decode(txBz); err != nil {
				continue
			}
			n := int64(len(txBz))
			if req.MaxTxBytes > 0 && size+n > req.MaxTxBytes {
				break
			}
			kept = append(kept, txBz)
			size += n
		}
		dropped := &abci.ResponsePrepareProposal{Txs: kept}
		if next == nil {
			return dropped, nil
		}
		req2 := *req
		req2.Txs = kept
		resp, err := next(ctx, &req2)
		if err != nil || resp == nil {
			return dropped, nil
		}
		return resp, nil
	}
}

// RejectMalformedProcessProposal rejects the proposal if any tx fails decode,
// then optionally calls the live default handler. Must not return err.
func RejectMalformedProcessProposal(decode sdk.TxDecoder, next sdk.ProcessProposalHandler) sdk.ProcessProposalHandler {
	return func(ctx sdk.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
		for _, txBz := range req.Txs {
			if _, err := decode(txBz); err != nil {
				return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_REJECT}, nil
			}
		}
		if next == nil {
			return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}, nil
		}
		resp, err := next(ctx, req)
		if err != nil || resp == nil {
			return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_REJECT}, nil
		}
		return resp, nil
	}
}
