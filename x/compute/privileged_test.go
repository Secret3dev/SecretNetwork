package compute_test

import (
	"context"
	"testing"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/cosmos/gogoproto/proto"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	v1wasmTypes "github.com/scrtlabs/SecretNetwork/go-cosmwasm/types/v1"
	"github.com/scrtlabs/SecretNetwork/x/compute/internal/keeper"
	"github.com/scrtlabs/SecretNetwork/x/compute/internal/types"
	"github.com/stretchr/testify/require"
)

func dummyGov() govkeeper.Keeper {
	return govkeeper.Keeper{}
}

func upgradeMsg() *types.MsgUpgradeProposalPassed {
	return &types.MsgUpgradeProposalPassed{
		SenderAddress: "secret1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq0d3dnb",
		MrEnclaveHash: make([]byte, 32),
	}
}

func TestMsgServerUpgradeProposalPassedAlwaysRejected(t *testing.T) {
	ms := keeper.NewMsgServerImpl(keeper.Keeper{})
	_, err := ms.UpgradeProposalPassed(context.Background(), upgradeMsg())
	require.ErrorIs(t, err, types.ErrUpgradeProposalPassedDenied)
}

func TestDenylistDirectUpgradeProposalPassed(t *testing.T) {
	enc := keeper.MakeEncodingConfig()
	c := keeper.NewPrivilegedChecker(enc.Codec, dummyGov())
	err := c.CheckMsg(sdk.Context{}, upgradeMsg(), 0)
	require.ErrorIs(t, err, types.ErrUpgradeProposalPassedDenied)
}

func TestDenylistPassedSoftwareUpgradeDoesNotAllowUpgradeProposalPassed(t *testing.T) {
	enc := keeper.MakeEncodingConfig()
	c := keeper.NewPrivilegedChecker(enc.Codec, dummyGov())
	su, err := codectypes.NewAnyWithValue(&upgradetypes.MsgSoftwareUpgrade{Plan: upgradetypes.Plan{Name: "v1.27.0"}})
	require.NoError(t, err)
	up, err := codectypes.NewAnyWithValue(upgradeMsg())
	require.NoError(t, err)
	err = c.CheckMsg(sdk.Context{}, &govv1.MsgSubmitProposal{Messages: []*codectypes.Any{su, up}}, 0)
	require.Error(t, err)
}

func TestSoftwareUpgradeTightnessLenMessages(t *testing.T) {
	enc := keeper.MakeEncodingConfig()
	c := keeper.NewPrivilegedChecker(enc.Codec, dummyGov())
	su, err := codectypes.NewAnyWithValue(&upgradetypes.MsgSoftwareUpgrade{Plan: upgradetypes.Plan{Name: "v1.27.0"}})
	require.NoError(t, err)
	vote, err := codectypes.NewAnyWithValue(&govv1.MsgVote{ProposalId: 1})
	require.NoError(t, err)
	err = c.CheckMsg(sdk.Context{}, &govv1.MsgSubmitProposal{Messages: []*codectypes.Any{su, vote}}, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "len(Messages)==1")
}

func TestMsgExecNestedUpgradeProposalPassed(t *testing.T) {
	enc := keeper.MakeEncodingConfig()
	c := keeper.NewPrivilegedChecker(enc.Codec, dummyGov())
	inner, err := codectypes.NewAnyWithValue(upgradeMsg())
	require.NoError(t, err)
	err = c.CheckMsg(sdk.Context{}, &authz.MsgExec{Msgs: []*codectypes.Any{inner}}, 0)
	require.ErrorIs(t, err, types.ErrUpgradeProposalPassedDenied)
}

func TestMsgGrantGenericAuthorizationString(t *testing.T) {
	enc := keeper.MakeEncodingConfig()
	c := keeper.NewPrivilegedChecker(enc.Codec, dummyGov())
	ga := authz.NewGenericAuthorization(types.MsgUpgradeProposalPassedTypeURL)
	anyAuth, err := codectypes.NewAnyWithValue(ga)
	require.NoError(t, err)
	err = c.CheckMsg(sdk.Context{}, &authz.MsgGrant{Grant: authz.Grant{Authorization: anyAuth}}, 0)
	require.ErrorIs(t, err, types.ErrUpgradeProposalPassedDenied)

	ga2 := authz.NewGenericAuthorization(types.MsgUpdateMachineWhitelistTypeURL)
	anyAuth2, err := codectypes.NewAnyWithValue(ga2)
	require.NoError(t, err)
	err = c.CheckMsg(sdk.Context{}, &authz.MsgGrant{Grant: authz.Grant{Authorization: anyAuth2}}, 0)
	require.Error(t, err)
}

func TestICARecvPacketInnerAnys(t *testing.T) {
	enc := keeper.MakeEncodingConfig()
	c := keeper.NewPrivilegedChecker(enc.Codec, dummyGov())
	txBz, err := icatypes.SerializeCosmosTx(enc.Codec, []proto.Message{upgradeMsg()}, icatypes.EncodingProtobuf)
	require.NoError(t, err)
	pd := icatypes.InterchainAccountPacketData{Type: icatypes.EXECUTE_TX, Data: txBz}
	err = c.CheckMsg(sdk.Context{}, &channeltypes.MsgRecvPacket{Packet: channeltypes.Packet{Data: pd.GetBytes()}}, 0)
	require.ErrorIs(t, err, types.ErrUpgradeProposalPassedDenied)
}

func TestStargateTypeURLDenied(t *testing.T) {
	enc := keeper.MakeEncodingConfig()
	encoders := keeper.DefaultEncoders(nil, enc.Codec)
	_, err := encoders.Stargate(sdk.AccAddress{}, &v1wasmTypes.StargateMsg{
		TypeURL: types.MsgUpgradeProposalPassedTypeURL,
		Value:   []byte{1},
	})
	require.ErrorIs(t, err, types.ErrUpgradeProposalPassedDenied)
	_, err = encoders.Stargate(sdk.AccAddress{}, &v1wasmTypes.StargateMsg{
		TypeURL: types.MsgUpdateMachineWhitelistTypeURL,
		Value:   []byte{1},
	})
	require.Error(t, err)
}

func TestT1Decoy81ByteInSameQueue(t *testing.T) {
	enc := keeper.MakeEncodingConfig()
	c := keeper.NewPrivilegedChecker(enc.Codec, dummyGov())

	decoyVal := make([]byte, types.MrenclaveProtoLen)
	decoyVal[0] = types.MrenclaveProtoPrefix0
	decoyVal[1] = types.MrenclaveProtoPrefix1
	decoyVal[types.MrenclaveProtoMidOff] = types.MrenclaveProtoMid0
	decoyVal[types.MrenclaveProtoMidOff+1] = types.MrenclaveProtoMid1
	require.True(t, types.LooksLikeMrenclaveProtoValue(decoyVal))

	decoyAny := &codectypes.Any{TypeUrl: "/cosmos.bank.v1beta1.MsgSend", Value: decoyVal}
	realAny, err := codectypes.NewAnyWithValue(upgradeMsg())
	require.NoError(t, err)

	err = c.CheckMsg(sdk.Context{}, &authz.MsgExec{Msgs: []*codectypes.Any{realAny, decoyAny}}, 0)
	require.ErrorIs(t, err, types.ErrUpgradeProposalPassedDenied)

	err = c.CheckMsg(sdk.Context{}, &authz.MsgExec{Msgs: []*codectypes.Any{decoyAny}}, 0)
	require.NoError(t, err)
}

func TestUnparseableNestedAnyRejected(t *testing.T) {
	enc := keeper.MakeEncodingConfig()
	c := keeper.NewPrivilegedChecker(enc.Codec, dummyGov())

	unknown := &codectypes.Any{TypeUrl: "/secret.compute.v1beta1.NotAType", Value: []byte{0x01, 0x02}}
	err := c.CheckMsg(sdk.Context{}, &authz.MsgExec{Msgs: []*codectypes.Any{unknown}}, 0)
	require.ErrorIs(t, err, types.ErrUnparseableNestedAny)

	gaAny := &codectypes.Any{TypeUrl: "/cosmos.authz.v1beta1.GenericAuthorization", Value: []byte{0xff, 0xfe}}
	err = c.CheckMsg(sdk.Context{}, &authz.MsgGrant{Grant: authz.Grant{Authorization: gaAny}}, 0)
	require.ErrorIs(t, err, types.ErrUnparseableNestedAny)

	inner := &codectypes.Any{TypeUrl: "/secret.compute.v1beta1.NotAType", Value: []byte{0x01}}
	cosmosTx := icatypes.CosmosTx{Messages: []*codectypes.Any{inner}}
	txBz, err := enc.Codec.Marshal(&cosmosTx)
	require.NoError(t, err)
	pd := icatypes.InterchainAccountPacketData{Type: icatypes.EXECUTE_TX, Data: txBz}
	err = c.CheckICAPacketData(sdk.Context{}, pd.GetBytes(), 0)
	require.ErrorIs(t, err, types.ErrUnparseableNestedAny)
}

func TestGovEndBlockSoftwareUpgradeTightness(t *testing.T) {
	enc := keeper.MakeEncodingConfig()
	c := keeper.NewPrivilegedChecker(enc.Codec, dummyGov())
	su, err := codectypes.NewAnyWithValue(&upgradetypes.MsgSoftwareUpgrade{Plan: upgradetypes.Plan{Name: "v1.27.0"}})
	require.NoError(t, err)
	vote, err := codectypes.NewAnyWithValue(&govv1.MsgVote{ProposalId: 1})
	require.NoError(t, err)
	err = c.CheckGovEndBlockMessages(sdk.Context{}, []*codectypes.Any{su, vote})
	require.Error(t, err)
	require.Contains(t, err.Error(), "len(Messages)==1")

	err = c.CheckGovEndBlockMessages(sdk.Context{}, []*codectypes.Any{su})
	require.NoError(t, err)

	up, err := codectypes.NewAnyWithValue(upgradeMsg())
	require.NoError(t, err)
	err = c.CheckGovEndBlockMessages(sdk.Context{}, []*codectypes.Any{up})
	require.ErrorIs(t, err, types.ErrUpgradeProposalPassedDenied)
}
