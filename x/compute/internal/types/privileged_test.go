package types

import (
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/stretchr/testify/require"
)

func TestPrivilegedTypeURLTable(t *testing.T) {
	require.True(t, IsUpgradeProposalPassedTypeURL(MsgUpgradeProposalPassedTypeURL))
	require.True(t, IsUpgradeProposalPassedTypeURL("secret.compute.v1beta1.MsgUpgradeProposalPassed"))
	require.True(t, IsUpgradeProposalPassedTypeURL(MsgUpgradeProposalPassedAminoName))
	require.True(t, IsUpgradeProposalPassedTypeURL(MsgUpgradeProposalPassedTypeName))
	require.True(t, IsUpdateMachineWhitelistTypeURL(MsgUpdateMachineWhitelistTypeURL))
	require.True(t, IsUpdateMachineWhitelistTypeURL(MsgUpdateMachineWhitelistAminoName))
	require.True(t, IsUpdateMachineWhitelistTypeURL(MsgUpdateMachineWhitelistTypeName))
	require.True(t, IsPrivilegedTypeURL(MsgUpgradeProposalPassedTypeURL))
	require.True(t, IsPrivilegedTypeURL(MsgUpdateMachineWhitelistTypeURL))
	require.False(t, IsPrivilegedTypeURL("/cosmos.bank.v1beta1.MsgSend"))
	require.False(t, IsPrivilegedTypeURL(""))
}

func TestAminoAndTypeNamesMatchSpec(t *testing.T) {
	require.Equal(t, "upgrade-proposal-passed", MsgUpgradeProposalPassed{}.Type())
	require.Equal(t, "update-machine-whitelist", MsgUpdateMachineWhitelist{}.Type())
	require.Equal(t, "wasm/MsgUpgradeProposalPassed", MsgUpgradeProposalPassedAminoName)
	require.Equal(t, "wasm/MsgUpdateMachineWhitelist", MsgUpdateMachineWhitelistAminoName)
}

func TestIsPrivilegedMsg(t *testing.T) {
	require.True(t, IsUpgradeProposalPassedMsg(&MsgUpgradeProposalPassed{}))
	require.True(t, IsUpdateMachineWhitelistMsg(&MsgUpdateMachineWhitelist{}))
	require.True(t, IsPrivilegedMsg(&MsgUpgradeProposalPassed{}))
	require.False(t, IsPrivilegedMsg(&MsgStoreCode{}))
}

func Test81ByteDecoyIsNotPrivilegedTypeURL(t *testing.T) {
	decoy := make([]byte, MrenclaveProtoLen)
	decoy[0] = MrenclaveProtoPrefix0
	decoy[1] = MrenclaveProtoPrefix1
	decoy[MrenclaveProtoMidOff] = MrenclaveProtoMid0
	decoy[MrenclaveProtoMidOff+1] = MrenclaveProtoMid1
	require.True(t, LooksLikeMrenclaveProtoValue(decoy))
	require.False(t, IsPrivilegedTypeURL(string(decoy)))

	real := MsgUpgradeProposalPassed{
		SenderAddress: "secret1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq0d3dnb",
		MrEnclaveHash: make([]byte, 32),
	}
	bz, err := real.Marshal()
	require.NoError(t, err)
	if LooksLikeMrenclaveProtoValue(bz) {
		require.NotEqual(t, decoy, bz)
	}
	any, err := codectypes.NewAnyWithValue(&real)
	require.NoError(t, err)
	require.True(t, IsUpgradeProposalPassedTypeURL(any.TypeUrl))
}
