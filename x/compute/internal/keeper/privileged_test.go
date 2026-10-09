package keeper

import (
	"testing"

	"cosmossdk.io/collections"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"

	"github.com/scrtlabs/SecretNetwork/x/compute/internal/types"
)

const whitelistMachineID = "00112233445566778899aabbccddeeff00112233"

// setupWhitelistProposal stores proposal 1 carrying a MsgUpdateMachineWhitelistProposal with the given status.
func setupWhitelistProposal(t *testing.T, status govv1.ProposalStatus) (sdk.Context, TestKeepers, govv1.Proposal) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures, nil, nil)
	gov := keepers.GovKeeper

	propMsg, err := codectypes.NewAnyWithValue(&types.MsgUpdateMachineWhitelistProposal{
		Authority: gov.GetAuthority(),
		MachineId: whitelistMachineID,
	})
	require.NoError(t, err)
	end := ctx.BlockTime()
	proposal := govv1.Proposal{
		Id:            1,
		Messages:      []*codectypes.Any{propMsg},
		Status:        status,
		VotingEndTime: &end,
	}
	require.NoError(t, gov.SetProposal(ctx, proposal))
	return ctx, keepers, proposal
}

func whitelistMsg(sender, machineID string) *types.MsgUpdateMachineWhitelist {
	return &types.MsgUpdateMachineWhitelist{Sender: sender, ProposalId: 1, MachineId: machineID}
}

func TestUpdateMachineWhitelistPassedProposal(t *testing.T) {
	ctx, keepers, _ := setupWhitelistProposal(t, govv1.StatusPassed)
	gov := keepers.GovKeeper

	c := NewPrivilegedChecker(MakeEncodingConfig().Codec, gov)
	require.NoError(t, c.CheckMsg(ctx, whitelistMsg(gov.GetAuthority(), whitelistMachineID), 0))
	require.ErrorContains(t, c.CheckMsg(ctx, whitelistMsg(gov.GetAuthority(), "00"), 0), "does not match the proposal")
}

func TestMsgServerUpdateMachineWhitelistAppliedOnce(t *testing.T) {
	ctx, keepers, proposal := setupWhitelistProposal(t, govv1.StatusPassed)
	require.NoError(t, keepers.WasmKeeper.SetMachineWhitelistApplied(ctx, proposal.Id))

	ms := NewMsgServerImpl(keepers.WasmKeeper)
	_, err := ms.UpdateMachineWhitelist(ctx, whitelistMsg(keepers.GovKeeper.GetAuthority(), whitelistMachineID))
	require.ErrorContains(t, err, "already applied")
}

func TestGovEndBlockKeepsWhitelistProposal(t *testing.T) {
	ctx, keepers, proposal := setupWhitelistProposal(t, govv1.StatusVotingPeriod)
	gov := keepers.GovKeeper
	require.NoError(t, gov.ActiveProposalsQueue.Set(ctx, collections.Join(*proposal.VotingEndTime, proposal.Id), proposal.Id))

	GuardGovEndBlock(ctx, MakeEncodingConfig().Codec, gov)

	got, err := gov.Proposals.Get(ctx, proposal.Id)
	require.NoError(t, err)
	require.Equal(t, govv1.StatusVotingPeriod, got.Status)
	require.Len(t, got.Messages, 1)
}
