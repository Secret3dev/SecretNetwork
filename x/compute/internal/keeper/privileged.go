package keeper

import (
	"fmt"
	"time"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/scrtlabs/SecretNetwork/x/compute/internal/types"
)

const privilegedWalkMaxDepth = 32

// PrivilegedChecker denylists privileged compute messages and re-checks gov.
type PrivilegedChecker struct {
	cdc      codec.Codec
	gov      govkeeper.Keeper
	govReady bool
}

func NewPrivilegedChecker(cdc codec.Codec, gov govkeeper.Keeper) PrivilegedChecker {
	return PrivilegedChecker{cdc: cdc, gov: gov, govReady: true}
}

func denylistChecker(cdc codec.Codec) PrivilegedChecker {
	return PrivilegedChecker{cdc: cdc, govReady: false}
}

func (c PrivilegedChecker) CheckTxMsgs(ctx sdk.Context, msgs []sdk.Msg) error {
	for _, msg := range msgs {
		if err := c.CheckMsg(ctx, msg, 0); err != nil {
			return err
		}
	}
	return nil
}

func (c PrivilegedChecker) CheckMsg(ctx sdk.Context, msg sdk.Msg, depth int) error {
	if msg == nil {
		return nil
	}
	if depth > privilegedWalkMaxDepth {
		return errorsmod.Wrap(types.ErrPrivilegedDenied, "nested Any depth exceeded")
	}

	if types.IsUpgradeProposalPassedMsg(msg) {
		return types.ErrUpgradeProposalPassedDenied
	}
	if m, ok := msg.(*types.MsgUpdateMachineWhitelist); ok {
		return c.checkUpdateMachineWhitelist(ctx, m)
	}
	if types.IsUpdateMachineWhitelistMsg(msg) {
		return errorsmod.Wrap(types.ErrPrivilegedDenied, types.MsgUpdateMachineWhitelistTypeURL)
	}

	switch m := msg.(type) {
	case *authz.MsgExec:
		return c.checkAnys(ctx, m.Msgs, depth+1)
	case *authz.MsgGrant:
		return c.checkGrant(ctx, m, depth+1)
	case *govv1.MsgSubmitProposal:
		return c.checkGovMessages(ctx, m.Messages, depth+1)
	case *govv1beta1.MsgSubmitProposal:
		if m.Content != nil {
			return c.checkAny(ctx, m.Content, depth+1)
		}
	case *channeltypes.MsgRecvPacket:
		return c.CheckICAPacketData(ctx, m.Packet.GetData(), depth+1)
	}
	return nil
}

func (c PrivilegedChecker) checkGrant(_ sdk.Context, m *authz.MsgGrant, _ int) error {
	if m == nil {
		return nil
	}
	authAny := m.Grant.Authorization
	if authAny == nil {
		return errorsmod.Wrap(types.ErrUnparseableNestedAny, "MsgGrant missing Authorization")
	}
	if types.IsPrivilegedTypeURL(authAny.TypeUrl) {
		return denyPrivileged(authAny.TypeUrl)
	}
	if authAny.GetTypeUrl() == "/cosmos.authz.v1beta1.GenericAuthorization" {
		return c.checkGenericAuthorizationAny(authAny)
	}
	if c.cdc == nil {
		return errorsmod.Wrap(types.ErrUnparseableNestedAny, authAny.TypeUrl)
	}
	var auth authz.Authorization
	if err := c.cdc.UnpackAny(authAny, &auth); err != nil {
		return errorsmod.Wrap(types.ErrUnparseableNestedAny, authAny.TypeUrl)
	}
	if auth != nil && types.IsPrivilegedTypeURL(auth.MsgTypeURL()) {
		return denyPrivileged(auth.MsgTypeURL())
	}
	return nil
}

func (c PrivilegedChecker) checkGenericAuthorizationAny(a *codectypes.Any) error {
	if a == nil {
		return errorsmod.Wrap(types.ErrUnparseableNestedAny, "GenericAuthorization")
	}
	if a.GetTypeUrl() != "/cosmos.authz.v1beta1.GenericAuthorization" {
		return nil
	}
	var ga authz.GenericAuthorization
	if err := ga.Unmarshal(a.Value); err != nil {
		if c.cdc == nil {
			return errorsmod.Wrap(types.ErrUnparseableNestedAny, "GenericAuthorization")
		}
		if err2 := c.cdc.Unmarshal(a.Value, &ga); err2 != nil {
			return errorsmod.Wrap(types.ErrUnparseableNestedAny, "GenericAuthorization")
		}
	}
	if ga.Msg == "" && len(a.Value) > 0 {
		return errorsmod.Wrap(types.ErrUnparseableNestedAny, "GenericAuthorization")
	}
	if types.IsPrivilegedTypeURL(ga.Msg) {
		return denyPrivileged(ga.Msg)
	}
	return nil
}

func (c PrivilegedChecker) checkGovMessages(ctx sdk.Context, messages []*codectypes.Any, depth int) error {
	hasSoftwareUpgrade := false
	for _, a := range messages {
		if a != nil && types.IsSoftwareUpgradeTypeURL(a.GetTypeUrl()) {
			hasSoftwareUpgrade = true
		}
	}
	if hasSoftwareUpgrade && len(messages) != 1 {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "SoftwareUpgrade tightness requires len(Messages)==1")
	}
	return c.checkAnys(ctx, messages, depth)
}

// CheckGovEndBlockMessages applies the same walk to gov EndBlock proposal Messages.
func (c PrivilegedChecker) CheckGovEndBlockMessages(ctx sdk.Context, messages []*codectypes.Any) error {
	return c.checkGovMessages(ctx, messages, 0)
}

// GuardGovEndBlock re-checks gov EndBlock messages before x/gov executes them.
// Must not return err (EndBlock err = committed halt). Violating proposals
// are failed and removed from the active queue so EndBlocker cannot execute them.
func GuardGovEndBlock(ctx sdk.Context, cdc codec.Codec, gov govkeeper.Keeper) {
	checker := NewPrivilegedChecker(cdc, gov)
	rng := collections.NewPrefixUntilPairRange[time.Time, uint64](ctx.BlockTime())

	type flagged struct {
		key collections.Pair[time.Time, uint64]
		id  uint64
		err error
	}
	var bad []flagged
	walkErr := gov.ActiveProposalsQueue.Walk(ctx, rng, func(key collections.Pair[time.Time, uint64], id uint64) (bool, error) {
		proposal, err := gov.Proposals.Get(ctx, id)
		if err != nil {
			return false, nil
		}
		if err := checker.CheckGovEndBlockMessages(ctx, proposal.Messages); err != nil {
			bad = append(bad, flagged{key: key, id: id, err: err})
		}
		return false, nil
	})
	if walkErr != nil {
		ctx.Logger().Error("gov EndBlock queue walk", "error", walkErr)
		return
	}
	for _, f := range bad {
		proposal, err := gov.Proposals.Get(ctx, f.id)
		if err != nil {
			continue
		}
		proposal.Messages = nil
		proposal.Status = govv1.StatusFailed
		proposal.FailedReason = f.err.Error()
		if err := gov.SetProposal(ctx, proposal); err != nil {
			ctx.Logger().Error("gov EndBlock SetProposal", "proposal", f.id, "error", err)
		}
		if err := gov.ActiveProposalsQueue.Remove(ctx, f.key); err != nil {
			ctx.Logger().Error("gov EndBlock queue remove", "proposal", f.id, "error", err)
		}
		if err := gov.RefundAndDeleteDeposits(ctx, proposal.Id); err != nil {
			ctx.Logger().Error("gov EndBlock refund", "proposal", f.id, "error", err)
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			govtypes.EventTypeActiveProposal,
			sdk.NewAttribute(govtypes.AttributeKeyProposalID, fmt.Sprintf("%d", proposal.Id)),
			sdk.NewAttribute(govtypes.AttributeKeyProposalResult, govtypes.AttributeValueProposalFailed),
		))
	}
}

func (c PrivilegedChecker) checkAnys(ctx sdk.Context, anys []*codectypes.Any, depth int) error {
	for _, a := range anys {
		if err := c.checkAny(ctx, a, depth); err != nil {
			return err
		}
	}
	return nil
}

func (c PrivilegedChecker) checkAny(ctx sdk.Context, a *codectypes.Any, depth int) error {
	if a == nil {
		return errorsmod.Wrap(types.ErrUnparseableNestedAny, "nil Any")
	}
	if types.IsUpgradeProposalPassedTypeURL(a.GetTypeUrl()) {
		return types.ErrUpgradeProposalPassedDenied
	}
	if types.IsUpdateMachineWhitelistTypeURL(a.GetTypeUrl()) {
		var msg types.MsgUpdateMachineWhitelist
		unmarshaled := false
		if err := msg.Unmarshal(a.Value); err == nil {
			unmarshaled = true
		} else if c.cdc != nil {
			if err2 := c.cdc.Unmarshal(a.Value, &msg); err2 == nil {
				unmarshaled = true
			}
		}
		if !unmarshaled {
			return errorsmod.Wrap(types.ErrUnparseableNestedAny, a.GetTypeUrl())
		}
		return c.checkUpdateMachineWhitelist(ctx, &msg)
	}
	if a.GetTypeUrl() == "/cosmos.authz.v1beta1.GenericAuthorization" {
		return c.checkGenericAuthorizationAny(a)
	}

	if c.cdc == nil {
		return errorsmod.Wrap(types.ErrUnparseableNestedAny, a.GetTypeUrl())
	}
	var sdkMsg sdk.Msg
	if err := c.cdc.UnpackAny(a, &sdkMsg); err != nil {
		return errorsmod.Wrap(types.ErrUnparseableNestedAny, a.GetTypeUrl())
	}
	return c.CheckMsg(ctx, sdkMsg, depth+1)
}

func (c PrivilegedChecker) CheckICAPacketData(ctx sdk.Context, packetData []byte, depth int) error {
	if len(packetData) == 0 {
		return nil
	}
	var data icatypes.InterchainAccountPacketData
	parsed := false
	if err := data.UnmarshalJSON(packetData); err == nil {
		parsed = true
	} else if err2 := data.Unmarshal(packetData); err2 == nil && data.Type != 0 {
		parsed = true
	}
	if !parsed {
		return nil
	}
	if data.Type != icatypes.EXECUTE_TX {
		return nil
	}
	return c.checkCosmosTxBytes(ctx, data.Data, depth)
}

func (c PrivilegedChecker) checkCosmosTxBytes(ctx sdk.Context, bz []byte, depth int) error {
	if len(bz) == 0 {
		return errorsmod.Wrap(types.ErrUnparseableNestedAny, "ica CosmosTx")
	}
	var cosmosTx icatypes.CosmosTx
	unmarshaled := false
	if c.cdc != nil {
		if err := c.cdc.Unmarshal(bz, &cosmosTx); err == nil {
			unmarshaled = true
		} else if err := c.cdc.UnmarshalJSON(bz, &cosmosTx); err == nil {
			unmarshaled = true
		}
	}
	if !unmarshaled {
		if err := cosmosTx.Unmarshal(bz); err != nil {
			return errorsmod.Wrap(types.ErrUnparseableNestedAny, "ica CosmosTx")
		}
	}
	return c.checkAnys(ctx, cosmosTx.Messages, depth)
}

func (c PrivilegedChecker) checkUpdateMachineWhitelist(ctx sdk.Context, msg *types.MsgUpdateMachineWhitelist) error {
	if msg == nil {
		return errorsmod.Wrap(types.ErrPrivilegedDenied, types.MsgUpdateMachineWhitelistTypeURL)
	}
	if !c.govReady {
		return errorsmod.Wrap(types.ErrPrivilegedDenied, types.MsgUpdateMachineWhitelistTypeURL)
	}
	proposal, err := c.gov.Proposals.Get(ctx, msg.ProposalId)
	if err != nil {
		return err
	}
	if proposal.Status != govv1.ProposalStatus_PROPOSAL_STATUS_PASSED {
		return sdkerrors.ErrInvalidRequest.Wrapf("proposal with id %d not passed", msg.ProposalId)
	}
	if len(proposal.Messages) != 1 {
		return sdkerrors.ErrInvalidRequest.Wrapf("proposal with id %d has %d messages, expected exactly 1", msg.ProposalId, len(proposal.Messages))
	}
	if proposal.Messages[0].GetTypeUrl() != types.MsgUpdateMachineWhitelistProposalTypeURL {
		return sdkerrors.ErrInvalidRequest.Wrapf("proposal with id %d is not of type MsgUpdateMachineWhitelistProposal", msg.ProposalId)
	}
	var proposalMsg types.MsgUpdateMachineWhitelistProposal
	if err = c.cdc.UnpackAny(proposal.Messages[0], &proposalMsg); err != nil {
		return err
	}
	if msg.MachineId != proposalMsg.MachineId {
		return sdkerrors.ErrInvalidRequest.Wrapf("machine id %s does not match the proposal %s", msg.MachineId, proposalMsg.MachineId)
	}
	return nil
}

func denyPrivileged(typeURL string) error {
	if types.IsUpgradeProposalPassedTypeURL(typeURL) {
		return types.ErrUpgradeProposalPassedDenied
	}
	return errorsmod.Wrap(types.ErrPrivilegedDenied, typeURL)
}

func CheckPrivilegedMsg(ctx sdk.Context, cdc codec.Codec, gov govkeeper.Keeper, msg sdk.Msg) error {
	return NewPrivilegedChecker(cdc, gov).CheckMsg(ctx, msg, 0)
}
