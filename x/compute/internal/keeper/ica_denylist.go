package keeper

import (
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	capabilitytypes "github.com/cosmos/ibc-go/modules/capability/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v8/modules/core/05-port/types"
	"github.com/cosmos/ibc-go/v8/modules/core/exported"
)

var _ porttypes.IBCModule = PrivilegedICAMiddleware{}

// PrivilegedICAMiddleware inspects ICA RecvPacket inner CosmosTx Anys.
type PrivilegedICAMiddleware struct {
	porttypes.IBCModule
	checker PrivilegedChecker
}

func NewPrivilegedICAMiddleware(app porttypes.IBCModule, cdc codec.Codec, gov govkeeper.Keeper) porttypes.IBCModule {
	return PrivilegedICAMiddleware{
		IBCModule: app,
		checker:   NewPrivilegedChecker(cdc, gov),
	}
}

func (m PrivilegedICAMiddleware) OnRecvPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	relayer sdk.AccAddress,
) exported.Acknowledgement {
	if err := m.checker.CheckICAPacketData(ctx, packet.GetData(), 0); err != nil {
		return channeltypes.NewErrorAcknowledgement(err)
	}
	return m.IBCModule.OnRecvPacket(ctx, packet, relayer)
}

func (m PrivilegedICAMiddleware) OnChanOpenInit(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionHops []string,
	portID string,
	channelID string,
	channelCap *capabilitytypes.Capability,
	counterparty channeltypes.Counterparty,
	version string,
) (string, error) {
	return m.IBCModule.OnChanOpenInit(ctx, order, connectionHops, portID, channelID, channelCap, counterparty, version)
}

func (m PrivilegedICAMiddleware) OnChanOpenTry(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionHops []string,
	portID, channelID string,
	channelCap *capabilitytypes.Capability,
	counterparty channeltypes.Counterparty,
	counterpartyVersion string,
) (string, error) {
	return m.IBCModule.OnChanOpenTry(ctx, order, connectionHops, portID, channelID, channelCap, counterparty, counterpartyVersion)
}

func (m PrivilegedICAMiddleware) OnChanOpenAck(ctx sdk.Context, portID, channelID, counterpartyChannelID, counterpartyVersion string) error {
	return m.IBCModule.OnChanOpenAck(ctx, portID, channelID, counterpartyChannelID, counterpartyVersion)
}

func (m PrivilegedICAMiddleware) OnChanOpenConfirm(ctx sdk.Context, portID, channelID string) error {
	return m.IBCModule.OnChanOpenConfirm(ctx, portID, channelID)
}

func (m PrivilegedICAMiddleware) OnChanCloseInit(ctx sdk.Context, portID, channelID string) error {
	return m.IBCModule.OnChanCloseInit(ctx, portID, channelID)
}

func (m PrivilegedICAMiddleware) OnChanCloseConfirm(ctx sdk.Context, portID, channelID string) error {
	return m.IBCModule.OnChanCloseConfirm(ctx, portID, channelID)
}

func (m PrivilegedICAMiddleware) OnAcknowledgementPacket(ctx sdk.Context, packet channeltypes.Packet, acknowledgement []byte, relayer sdk.AccAddress) error {
	return m.IBCModule.OnAcknowledgementPacket(ctx, packet, acknowledgement, relayer)
}

func (m PrivilegedICAMiddleware) OnTimeoutPacket(ctx sdk.Context, packet channeltypes.Packet, relayer sdk.AccAddress) error {
	return m.IBCModule.OnTimeoutPacket(ctx, packet, relayer)
}

func (m PrivilegedICAMiddleware) OnChanUpgradeInit(ctx sdk.Context, portID, channelID string, order channeltypes.Order, hops []string, version string) (string, error) {
	if u, ok := m.IBCModule.(porttypes.UpgradableModule); ok {
		return u.OnChanUpgradeInit(ctx, portID, channelID, order, hops, version)
	}
	return version, nil
}

func (m PrivilegedICAMiddleware) OnChanUpgradeTry(ctx sdk.Context, portID, channelID string, order channeltypes.Order, hops []string, counterpartyVersion string) (string, error) {
	if u, ok := m.IBCModule.(porttypes.UpgradableModule); ok {
		return u.OnChanUpgradeTry(ctx, portID, channelID, order, hops, counterpartyVersion)
	}
	return counterpartyVersion, nil
}

func (m PrivilegedICAMiddleware) OnChanUpgradeAck(ctx sdk.Context, portID, channelID, counterpartyVersion string) error {
	if u, ok := m.IBCModule.(porttypes.UpgradableModule); ok {
		return u.OnChanUpgradeAck(ctx, portID, channelID, counterpartyVersion)
	}
	return nil
}

func (m PrivilegedICAMiddleware) OnChanUpgradeOpen(ctx sdk.Context, portID, channelID string, order channeltypes.Order, hops []string, version string) {
	if u, ok := m.IBCModule.(porttypes.UpgradableModule); ok {
		u.OnChanUpgradeOpen(ctx, portID, channelID, order, hops, version)
	}
}

var _ porttypes.UpgradableModule = PrivilegedICAMiddleware{}
