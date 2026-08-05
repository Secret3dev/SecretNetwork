//go:build secretcli

// Handler-level tests for the Community Continuance upgrade.
//
// These drive the REAL handler through x/upgrade's PreBlocker on a real
// SecretNetworkApp, which is the same code path a validator's node takes at the
// upgrade height. Everything here is invisible to config_test.go, which cannot
// import keepers by design.
//
// Run with the `secretcli` build tag, which selects the repo's mock enclave FFI
// (go-cosmwasm/api/lib_mock.go). No SGX SDK, no signed enclave, no Docker:
//
//	go test -count 1 -tags secretcli ./app/upgrades/v1.26/ -v
//
// Pattern: ScheduleUpgrade at H-1, advance to H, call PreBlock, assert.
//
// This lives in an EXTERNAL test package (v1_26_test) because `app` imports
// this package — an internal test would be an import cycle.
package v1_26_test

import (
	"os"
	"strings"
	"testing"
	"time"

	coreheader "cosmossdk.io/core/header"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/x/upgrade"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"

	"github.com/scrtlabs/SecretNetwork/app"
	"github.com/scrtlabs/SecretNetwork/app/upgrades/v1.26/config"
	scrt "github.com/scrtlabs/SecretNetwork/types"
	"github.com/scrtlabs/SecretNetwork/x/compute"
)

const upgradeHeight = 10

// TestMain sets the bech32 prefixes the app expects. cmd/secretd/root.go does
// this at startup and then Seal()s; a test must not seal, because the SDK's
// global config is shared across tests in the binary.
func TestMain(m *testing.M) {
	c := sdk.GetConfig()
	c.SetCoinType(scrt.CoinType)
	c.SetPurpose(scrt.CoinPurpose)
	c.SetBech32PrefixForAccount(scrt.Bech32PrefixAccAddr, scrt.Bech32PrefixAccPub)
	c.SetBech32PrefixForValidator(scrt.Bech32PrefixValAddr, scrt.Bech32PrefixValPub)
	c.SetBech32PrefixForConsensusNode(scrt.Bech32PrefixConsAddr, scrt.Bech32PrefixConsPub)
	os.Exit(m.Run())
}

// newTestApp builds a real SecretNetworkApp over an in-memory DB for the given
// chain ID, with the enclave disabled.
func newTestApp(t *testing.T, chainID string) (*app.SecretNetworkApp, sdk.Context) {
	t.Helper()

	dir, err := os.MkdirTemp("", "continuance-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	wasmConfig := compute.DefaultWasmConfig()
	wasmConfig.InitEnclave = false

	a := app.NewSecretNetworkApp(
		log.NewNopLogger(), dbm.NewMemDB(), nil, true, true,
		simtestutil.NewAppOptionsWithFlagHome(dir), wasmConfig,
		baseapp.SetChainID(chainID),
	)

	if _, err := a.InitChain(initChainReq(chainID, initChainGenesis(t, a))); err != nil {
		t.Fatalf("InitChain: %v", err)
	}
	// InitChain writes into finalizeBlockState, not the committed store, and
	// FinalizeBlock is not usable here (Secret's block hooks need a real
	// enclave). NewContextLegacy reads finalizeBlockState, which is exactly
	// the genesis state we just wrote.
	//
	// A real block time matters: vesting schedules are anchored to it, and a
	// zero time yields negative Unix timestamps the SDK rejects.
	ctx := a.BaseApp.NewContextLegacy(false, cmtproto.Header{
		ChainID: chainID,
		Height:  1,
		Time:    time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
	})
	return a, ctx
}

// runUpgrade schedules the plan at H-1 and drives PreBlock at H — the same path
// x/upgrade takes on a real node. Returns the PreBlock error, if any.
func runUpgrade(t *testing.T, a *app.SecretNetworkApp, ctx sdk.Context) error {
	t.Helper()

	ctx = ctx.WithBlockHeight(upgradeHeight - 1)
	plan := upgradetypes.Plan{Name: config.UpgradeName, Height: upgradeHeight}
	if err := a.AppKeepers.UpgradeKeeper.ScheduleUpgrade(ctx, plan); err != nil {
		t.Fatalf("ScheduleUpgrade: %v", err)
	}
	if _, err := a.AppKeepers.UpgradeKeeper.GetUpgradePlan(ctx); err != nil {
		t.Fatalf("GetUpgradePlan: %v", err)
	}

	ctx = ctx.WithHeaderInfo(coreheader.Info{
		Height: upgradeHeight,
		Time:   ctx.BlockTime().Add(time.Second),
	}).WithBlockHeight(upgradeHeight)

	mod := upgrade.NewAppModule(a.AppKeepers.UpgradeKeeper, addresscodec.NewBech32Codec(scrt.Bech32PrefixAccAddr))
	_, err := mod.PreBlock(ctx)
	return err
}

// ---------------------------------------------------------------------------
// Happy path
// ---------------------------------------------------------------------------

// TestUpgradeMintsAndVests is the test that did not exist: it proves the mint
// actually executes and every bucket lands with the right balance, account type
// and lockup — against real keeper state, not re-derived from constants.
func TestUpgradeMintsAndVests(t *testing.T) {
	n := filledNetwork(t, "secret-4")
	restore := swapMainnet(n)
	defer restore()

	a, ctx := newTestApp(t, "secret-4")
	seedAddresses(t, a, ctx, n) // rehearses the real seeding rule

	supplyBefore := a.AppKeepers.BankKeeper.GetSupply(ctx, config.BondDenom).Amount
	mintAddr := a.AppKeepers.AccountKeeper.GetModuleAddress(minttypes.ModuleName)
	mintBefore := a.AppKeepers.BankKeeper.GetBalance(ctx, mintAddr, config.BondDenom).Amount

	if err := runUpgrade(t, a, ctx); err != nil {
		t.Fatalf("upgrade failed: %v", err)
	}

	// Supply rose by exactly the announced mint. Note this is measured across
	// PreBlock only — on a live chain x/mint's BeginBlocker adds one block
	// provision in the same block, which is why an operator's supply check
	// must allow for that term (see mainnet/RUNBOOK Phase 7).
	supplyAfter := a.AppKeepers.BankKeeper.GetSupply(ctx, config.BondDenom).Amount
	wantDelta := config.ToUscrt(config.TotalMintSCRT)
	if got := supplyAfter.Sub(supplyBefore); !got.Equal(wantDelta) {
		t.Fatalf("supply delta = %s, want %s", got, wantDelta)
	}

	// The mint module must be empty again — the real invariant, read from state.
	if got := a.AppKeepers.BankKeeper.GetBalance(ctx, mintAddr, config.BondDenom).Amount; !got.Equal(mintBefore) {
		t.Fatalf("mint module balance = %s, want %s (nothing may be left behind)", got, mintBefore)
	}

	// Per-bucket: balance, account type, and locked amount.
	one := sdkmath.OneInt() // the 1 uscrt seed
	for _, alloc := range n.Allocations() {
		addr := sdk.MustAccAddressFromBech32(alloc.Address)
		gotBal := a.AppKeepers.BankKeeper.GetBalance(ctx, addr, config.BondDenom).Amount
		wantBal := config.ToUscrt(alloc.TotalSCRT).Add(one)
		if !gotBal.Equal(wantBal) {
			t.Errorf("%s: balance = %s, want %s (allocation + 1 uscrt seed)", alloc.Name, gotBal, wantBal)
		}

		acc := a.AppKeepers.AccountKeeper.GetAccount(ctx, addr)
		switch alloc.Kind {
		case config.VestNone:
			if _, ok := acc.(*authtypes.BaseAccount); !ok {
				t.Errorf("%s: account type = %T, want *authtypes.BaseAccount (fully liquid)", alloc.Name, acc)
			}
		case config.VestContinuous:
			cva, ok := acc.(*vestingtypes.ContinuousVestingAccount)
			if !ok {
				t.Errorf("%s: account type = %T, want *ContinuousVestingAccount", alloc.Name, acc)
				continue
			}
			if got := cva.OriginalVesting.AmountOf(config.BondDenom); !got.Equal(alloc.Locked()) {
				t.Errorf("%s: original_vesting = %s, want %s", alloc.Name, got, alloc.Locked())
			}
		case config.VestPeriodicQuarterly:
			pva, ok := acc.(*vestingtypes.PeriodicVestingAccount)
			if !ok {
				t.Errorf("%s: account type = %T, want *PeriodicVestingAccount", alloc.Name, acc)
				continue
			}
			if got := pva.OriginalVesting.AmountOf(config.BondDenom); !got.Equal(alloc.Locked()) {
				t.Errorf("%s: original_vesting = %s, want %s", alloc.Name, got, alloc.Locked())
			}
			// Advisors: 16 quarterly unlocks over 4 years (v24).
			wantPeriods := alloc.DurationMonths / 3
			if len(pva.VestingPeriods) != wantPeriods {
				t.Errorf("%s: %d vesting periods, want %d (%d months / 3)",
					alloc.Name, len(pva.VestingPeriods), wantPeriods, alloc.DurationMonths)
			}
		}
	}

	// Validator program: no seats filled here, so the whole 72M lands at the
	// program address with 64.8M locked and the seat reserves liquid.
	vp := n.ValidatorProgram()
	progAddr := sdk.MustAccAddressFromBech32(vp.ProgramAddress)
	gotProg := a.AppKeepers.BankKeeper.GetBalance(ctx, progAddr, config.BondDenom).Amount
	wantProg := config.ToUscrt(vp.TotalSCRT).Add(one)
	if !gotProg.Equal(wantProg) {
		t.Errorf("program address balance = %s, want %s", gotProg, wantProg)
	}
	pva, ok := a.AppKeepers.AccountKeeper.GetAccount(ctx, progAddr).(*vestingtypes.PeriodicVestingAccount)
	if !ok {
		t.Fatalf("program address is %T, want *PeriodicVestingAccount", a.AppKeepers.AccountKeeper.GetAccount(ctx, progAddr))
	}
	if got := pva.OriginalVesting.AmountOf(config.BondDenom); !got.Equal(config.ToUscrt(vp.VestingSCRT())) {
		t.Errorf("program original_vesting = %s, want %s", got, config.ToUscrt(vp.VestingSCRT()))
	}

	// Monetary policy.
	mp, err := a.AppKeepers.MintKeeper.Params.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := sdkmath.LegacyMustNewDecFromStr(config.TargetInflation)
	if !mp.InflationMin.Equal(want) || !mp.InflationMax.Equal(want) {
		t.Errorf("inflation min/max = %s/%s, want %s/%s", mp.InflationMin, mp.InflationMax, want, want)
	}
	if !mp.InflationRateChange.IsZero() {
		t.Errorf("inflation_rate_change = %s, want 0", mp.InflationRateChange)
	}
	dp, err := a.AppKeepers.DistrKeeper.Params.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !dp.SecretFoundationTax.IsZero() {
		t.Errorf("secret_foundation_tax = %s, want 0", dp.SecretFoundationTax)
	}
}

// ---------------------------------------------------------------------------
// Negative paths — these are the ones that matter
// ---------------------------------------------------------------------------

// TestUpgradeRefusesUnknownChain proves the runtime network guard through the
// real execution path: on a chain we have no configuration for, the handler
// refuses rather than guessing whose custody addresses apply.
func TestUpgradeRefusesUnknownChain(t *testing.T) {
	a, ctx := newTestApp(t, "not-a-real-chain-1")
	supplyBefore := a.AppKeepers.BankKeeper.GetSupply(ctx, config.BondDenom).Amount

	err := runUpgrade(t, a, ctx)
	if err == nil {
		t.Fatal("expected the upgrade to fail on an unknown chain ID")
	}
	if supplyAfter := a.AppKeepers.BankKeeper.GetSupply(ctx, config.BondDenom).Amount; !supplyAfter.Equal(supplyBefore) {
		t.Fatalf("supply changed on a refused upgrade: %s -> %s", supplyBefore, supplyAfter)
	}
}

// TestUpgradeRefusesPlaceholders proves fail-closed on our own misconfiguration
// through the real path, not by re-running Validate in isolation.
func TestUpgradeRefusesPlaceholders(t *testing.T) {
	a, ctx := newTestApp(t, "secret-4") // shipped Mainnet still has FillMe
	supplyBefore := a.AppKeepers.BankKeeper.GetSupply(ctx, config.BondDenom).Amount

	err := runUpgrade(t, a, ctx)
	if err == nil {
		t.Fatal("expected the upgrade to refuse a placeholder address table")
	}
	if supplyAfter := a.AppKeepers.BankKeeper.GetSupply(ctx, config.BondDenom).Amount; !supplyAfter.Equal(supplyBefore) {
		t.Fatalf("supply changed on a refused upgrade: %s -> %s", supplyBefore, supplyAfter)
	}
}

// TestNonBaseAccountAtBucketHaltsUpgrade: every bucket address must already be
// a plain BaseAccount. A vesting (or other) account type at that address must
// fail closed before any mint is applied.
func TestNonBaseAccountAtBucketHaltsUpgrade(t *testing.T) {
	n := filledNetwork(t, "secret-4")
	restore := swapMainnet(n)
	defer restore()

	a, ctx := newTestApp(t, "secret-4")

	victim := sdk.MustAccAddressFromBech32(n.Addresses.Foundation)
	fresh := a.AppKeepers.AccountKeeper.NewAccountWithAddress(ctx, victim)
	base, ok := fresh.(*authtypes.BaseAccount)
	if !ok {
		t.Fatalf("expected a BaseAccount, got %T", fresh)
	}
	wrongType, err := vestingtypes.NewContinuousVestingAccount(
		base,
		sdk.NewCoins(sdk.NewCoin(config.BondDenom, sdkmath.OneInt())),
		ctx.BlockTime().Unix(),
		ctx.BlockTime().Add(time.Hour).Unix(),
	)
	if err != nil {
		t.Fatal(err)
	}
	a.AppKeepers.AccountKeeper.SetAccount(ctx, wrongType)

	seedAddressesExcept(t, a, ctx, n, n.Addresses.Foundation)

	if err := runUpgrade(t, a, ctx); err == nil {
		t.Fatal("expected non-BaseAccount at bucket address to halt the upgrade")
	}
}

// TestSeededBaseAccountsUpgradeCleanly: with every custody address pre-seeded
// as a BaseAccount (operational requirement), the upgrade succeeds.
func TestSeededBaseAccountsUpgradeCleanly(t *testing.T) {
	n := filledNetwork(t, "secret-4")
	restore := swapMainnet(n)
	defer restore()

	a, ctx := newTestApp(t, "secret-4")
	seedAddresses(t, a, ctx, n)

	if err := runUpgrade(t, a, ctx); err != nil {
		t.Fatalf("seeded addresses should upgrade cleanly, got: %v", err)
	}
}

// TestUnseededBucketHaltsUpgrade: a missing BaseAccount at a vesting bucket
// must halt. requireSeededBaseAccount refuses to create accounts.
func TestUnseededBucketHaltsUpgrade(t *testing.T) {
	n := filledNetwork(t, "secret-4")
	restore := swapMainnet(n)
	defer restore()

	a, ctx := newTestApp(t, "secret-4")
	seedAddressesExcept(t, a, ctx, n, n.Addresses.Foundation)

	err := runUpgrade(t, a, ctx)
	if err == nil {
		t.Fatal("an UNSEEDED bucket address must halt the upgrade")
	}
	if !strings.Contains(err.Error(), "NO ACCOUNT") {
		t.Fatalf("expected the unseeded-address error, got: %v", err)
	}
}

// TestUnseededLiquidBucketHaltsUpgrade: same rule for VestNone buckets (the
// send path would otherwise create the recipient account).
func TestUnseededLiquidBucketHaltsUpgrade(t *testing.T) {
	n := filledNetwork(t, "secret-4")
	restore := swapMainnet(n)
	defer restore()

	a, ctx := newTestApp(t, "secret-4")
	seedAddressesExcept(t, a, ctx, n, n.Addresses.EcosystemFund)

	err := runUpgrade(t, a, ctx)
	if err == nil {
		t.Fatal("an unseeded VestNone bucket must halt the upgrade")
	}
	if !strings.Contains(err.Error(), "NO ACCOUNT") {
		t.Fatalf("expected the unseeded-address error, got: %v", err)
	}
}
