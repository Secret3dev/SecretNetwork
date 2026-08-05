//go:build secretcli

package v1_26_test

import (
	"encoding/json"
	"testing"

	sdkmath "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/scrtlabs/SecretNetwork/app"
	"github.com/scrtlabs/SecretNetwork/app/upgrades/v1.26/config"
)

// funderAddr is the genesis account the tests spend from when seeding.
var funderAddr sdk.AccAddress

// initChainGenesis builds a genesis with exactly one validator plus a
// well-funded account, and returns the marshalled app state. InitChain refuses
// an empty validator set.
func initChainGenesis(t *testing.T, a *app.SecretNetworkApp) []byte {
	t.Helper()

	valPriv := cmted25519.GenPrivKey()
	val := cmttypes.NewValidator(valPriv.PubKey(), 1)
	valSet := cmttypes.NewValidatorSet([]*cmttypes.Validator{val})

	funderPriv := secp256k1.GenPrivKey()
	funder := authtypes.NewBaseAccount(funderPriv.PubKey().Address().Bytes(), funderPriv.PubKey(), 0, 0)
	funderAddr = funder.GetAddress()

	// Enough to seed every address and still cover the validator bond.
	balance := banktypes.Balance{
		Address: funder.GetAddress().String(),
		Coins:   sdk.NewCoins(sdk.NewCoin(config.BondDenom, sdkmath.NewInt(1_000_000_000_000))),
	}

	genState, err := simtestutil.GenesisStateWithValSet(
		a.AppCodec(), app.NewDefaultGenesisState(), valSet,
		[]authtypes.GenesisAccount{funder}, balance,
	)
	if err != nil {
		t.Fatalf("GenesisStateWithValSet: %v", err)
	}

	stateBytes, err := json.Marshal(genState)
	if err != nil {
		t.Fatalf("marshal genesis: %v", err)
	}
	return stateBytes
}

// initChainReq is the InitChain request used by every test.
func initChainReq(chainID string, stateBytes []byte) *abci.RequestInitChain {
	return &abci.RequestInitChain{
		ChainId:         chainID,
		ConsensusParams: simtestutil.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
		InitialHeight:   1,
	}
}

// ---------------------------------------------------------------------------
// Network fixtures
// ---------------------------------------------------------------------------

// testAddr builds a deterministic, valid 20-byte secret1 address.
func testAddr(seed byte) string {
	b := make([]byte, 20)
	for i := range b {
		b[i] = seed
	}
	return sdk.AccAddress(b).String()
}

// filledNetwork returns a Network for chainID with real (throwaway) addresses,
// so the handler can run to completion. The shipped tables still hold FillMe.
func filledNetwork(t *testing.T, chainID string) config.Network {
	t.Helper()
	n, err := config.ForChainID(chainID)
	if err != nil {
		t.Fatalf("ForChainID(%q): %v", chainID, err)
	}
	n.Addresses = config.AddressSet{
		Foundation:            testAddr(0x11),
		CoreDevelopment:       testAddr(0x22),
		Advisors:              testAddr(0x33),
		EcosystemFund:         testAddr(0x44),
		ResearchDevelopment:   testAddr(0x55),
		BuilderRelayerSupport: testAddr(0x66),
		Remediation:           testAddr(0x77),
		ValidatorProgram:      testAddr(0x88),
	}
	if err := config.Validate(n); err != nil {
		t.Fatalf("test fixture is not valid: %v", err)
	}
	return n
}

// swapMainnet temporarily replaces config.Mainnet (and the Networks slice that
// ForChainID reads) so the handler picks up a filled address table. Returns a
// restore func; tests must defer it.
//
// This is the one piece of global mutation in these tests. It exists because
// the shipped tables intentionally hold placeholders until real custody
// addresses are generated.
func swapMainnet(n config.Network) func() {
	origMainnet := config.Mainnet
	origNetworks := config.Networks

	config.Mainnet = n
	swapped := make([]config.Network, len(origNetworks))
	copy(swapped, origNetworks)
	for i := range swapped {
		if swapped[i].ChainID == n.ChainID {
			swapped[i] = n
		}
	}
	config.Networks = swapped

	return func() {
		config.Mainnet = origMainnet
		config.Networks = origNetworks
	}
}

// ---------------------------------------------------------------------------
// Seeding — custody addresses must exist as BaseAccounts before the upgrade
// ---------------------------------------------------------------------------

// seedAddresses credits every custody address with 1 uscrt so a BaseAccount
// exists. requireSeededBaseAccount refuses unseeded addresses.
func seedAddresses(t *testing.T, a *app.SecretNetworkApp, ctx sdk.Context, n config.Network) {
	t.Helper()
	seedAddressesExcept(t, a, ctx, n, "")
}

// seedAddressesExcept seeds every custody address except the one named.
func seedAddressesExcept(t *testing.T, a *app.SecretNetworkApp, ctx sdk.Context, n config.Network, skip string) {
	t.Helper()

	addrs := make([]string, 0, 8)
	for _, alloc := range n.Allocations() {
		addrs = append(addrs, alloc.Address)
	}
	addrs = append(addrs, n.ValidatorProgram().ProgramAddress)

	one := sdk.NewCoins(sdk.NewCoin(config.BondDenom, sdkmath.OneInt()))
	for _, s := range addrs {
		if s == skip {
			continue
		}
		to := sdk.MustAccAddressFromBech32(s)
		if err := a.AppKeepers.BankKeeper.SendCoins(ctx, funderAddr, to, one); err != nil {
			t.Fatalf("seeding %s: %v", s, err)
		}
		// A plain send must produce an ordinary BaseAccount — that is the whole
		// point of the rule, so assert it rather than assume it.
		if _, ok := a.AppKeepers.AccountKeeper.GetAccount(ctx, to).(*authtypes.BaseAccount); !ok {
			t.Fatalf("seeding %s did not produce a BaseAccount", s)
		}
	}
}
