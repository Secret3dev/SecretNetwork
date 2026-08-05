// Package v1_26 implements the Community Continuance upgrade: a one-time mint
// of 1,079,000,000 SCRT distributed across the proposal's buckets with on-chain
// vesting, a validator program, inflation pinned at 5%, and the Secret
// foundation tax set to 0.
//
// Design rules this handler is built to, in priority order:
//
//  1. NEVER mint on the wrong chain. The chain ID is asserted before any state
//     is touched, and the handler fails closed.
//  2. NEVER halt on data an outsider or a validator controls. Seat payouts
//     degrade to "vacant" rather than returning an error.
//  3. Halt LOUDLY on our own misconfiguration, which is caught at build time by
//     config.Validate and re-checked here.
package v1_26

import (
	"context"
	"fmt"
	"time"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"

	"github.com/scrtlabs/SecretNetwork/app/keepers"
	"github.com/scrtlabs/SecretNetwork/app/upgrades"
	"github.com/scrtlabs/SecretNetwork/app/upgrades/v1.26/config"
)

// UpgradeName is the on-chain upgrade name. It is a CONSTANT, identical on
// every chain, so a binary's on-chain identity cannot depend on a build-time
// switch. It must match the governance plan name byte-for-byte or every node
// halts at the height with no handler registered.
const UpgradeName = config.UpgradeName

var Upgrade = upgrades.Upgrade{
	UpgradeName:          UpgradeName,
	CreateUpgradeHandler: createUpgradeHandler,
	StoreUpgrades:        storetypes.StoreUpgrades{},
}

func createUpgradeHandler(
	mm *module.Manager,
	k *keepers.SecretAppKeepers,
	configurator module.Configurator,
) upgradetypes.UpgradeHandler {
	return func(goCtx context.Context, _ upgradetypes.Plan, vm module.VersionMap) (module.VersionMap, error) {
		ctx := sdk.UnwrapSDKContext(goCtx)
		logger := ctx.Logger().With("upgrade", UpgradeName)

		logger.Info("running module migrations...")
		newVM, err := mm.RunMigrations(goCtx, configurator, vm)
		if err != nil {
			return nil, err
		}

		if err := executeContinuance(ctx, k, logger); err != nil {
			// Returning an error fails FinalizeBlock identically on every node,
			// halting the chain with nothing committed. That is the correct
			// failure mode for a misconfigured mint: no partial execution.
			return nil, fmt.Errorf("community continuance execution failed: %w", err)
		}

		return newVM, nil
	}
}

// ---------------------------------------------------------------------------
// Execution
// ---------------------------------------------------------------------------

func executeContinuance(ctx sdk.Context, k *keepers.SecretAppKeepers, logger log.Logger) error {
	// 1. NETWORK SELECTION — before any state is read or written.
	//
	// The custody addresses are chosen from the chain we are ACTUALLY on, not
	// from a build-time variable. An unknown chain is a hard error: the handler
	// refuses to mint rather than guessing whose addresses apply. Failing here
	// halts without minting, which is vastly preferable to minting 1.079B SCRT
	// to the wrong custody.
	n, err := config.ForChainID(ctx.ChainID())
	if err != nil {
		return err
	}

	// 2. Config gate — the same check the release test runs at build time.
	if err := config.Validate(n); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	now := ctx.BlockTime().UTC()
	logger.Info("executing community continuance",
		"network", n.Name, "chain_id", n.ChainID, "block_time", now.Format(time.RFC3339))

	// Calendar-month normalization: a block on the 29th–31st shifts every
	// cliffed schedule by a day or three (Aug 31 + 6mo → Mar 3). Harmless
	// mechanically, but published unlock dates must come from the real
	// schedule, not from an assumed day-of-month.
	if now.Day() > 28 {
		logger.Warn("upgrade block falls after the 28th — calendar-month normalization will shift cliffed schedules; publish dates from actual on-chain values",
			"block_day", fmt.Sprintf("%d", now.Day()))
	}

	// 3. Baseline the mint module balance. Comparing back to this after
	// distribution is a REAL invariant: it reads chain state, unlike a check
	// that re-derives both sides from the same constants.
	mintModuleAddr := k.AccountKeeper.GetModuleAddress(minttypes.ModuleName)
	if mintModuleAddr == nil {
		return fmt.Errorf("mint module account address not found")
	}
	baseline := k.BankKeeper.GetBalance(ctx, mintModuleAddr, config.BondDenom)

	// 4. Mint the announced supply. config.Validate has already proven the
	// bucket table sums to exactly this.
	totalMint := config.ToUscrt(config.TotalMintSCRT)
	mintCoins := sdk.NewCoins(sdk.NewCoin(config.BondDenom, totalMint))
	if err := k.BankKeeper.MintCoins(ctx, minttypes.ModuleName, mintCoins); err != nil {
		return fmt.Errorf("minting %s: %w", mintCoins, err)
	}
	logger.Info("minted continuance supply", "amount", mintCoins.String())

	// 5. Buckets: configure vesting, then fund.
	for _, a := range n.Allocations() {
		if err := fundBucket(ctx, k, logger, now, a); err != nil {
			return err
		}
	}

	// 6. Validator program.
	if err := executeValidatorProgram(ctx, k, logger, now, n); err != nil {
		return fmt.Errorf("validator program: %w", err)
	}

	// 7. INVARIANT: the mint module must hold exactly what it held before.
	// Any coin minted and not distributed — or distributed twice — shows up
	// here as a mismatch and halts before the upgrade completes.
	final := k.BankKeeper.GetBalance(ctx, mintModuleAddr, config.BondDenom)
	if !final.Equal(baseline) {
		return fmt.Errorf("distribution invariant violated: mint module held %s before and %s after (expected them to match); minted %s",
			baseline.String(), final.String(), mintCoins.String())
	}
	logger.Info("distribution invariant satisfied", "mint_module_balance", final.String())

	// 8. Monetary policy: pin inflation at 5%.
	if err := pinInflation(ctx, k); err != nil {
		return fmt.Errorf("setting inflation: %w", err)
	}
	logger.Info("inflation pinned", "rate", config.TargetInflation)

	// 9. Secret foundation tax → 0%.
	if config.ZeroFoundationTax {
		if err := zeroFoundationTax(ctx, k, logger); err != nil {
			return err
		}
	}

	logger.Info("community continuance executed", "minted", mintCoins.String())
	return nil
}

// fundBucket configures the bucket's vesting schedule (if any) and transfers
// its full allocation from the mint module.
func fundBucket(ctx sdk.Context, k *keepers.SecretAppKeepers, logger log.Logger, now time.Time, a config.Allocation) error {
	addr, err := sdk.AccAddressFromBech32(a.Address)
	if err != nil {
		return fmt.Errorf("bucket %s: %w", a.Name, err) // unreachable; Validate ran
	}
	total := config.ToUscrt(a.TotalSCRT)
	locked := a.Locked()

	// The address must ALREADY EXIST as an ordinary BaseAccount. Checked for
	// every bucket including VestNone, before anything is written. Enforces
	// the operational seeding rule in code: unseeded or wrong-type addresses
	// halt rather than creating and funding a new account.
	if err := requireSeededBaseAccount(ctx, k.AccountKeeper, addr, "bucket "+a.Name); err != nil {
		return err
	}

	switch a.Kind {
	case config.VestNone:
		// Fully liquid — no vesting wrapper. Seed check above still applies.

	case config.VestContinuous:
		start, end := config.ContinuousWindow(now, a)
		lockedCoins := sdk.NewCoins(sdk.NewCoin(config.BondDenom, locked))
		if err := setContinuousVesting(ctx, k.AccountKeeper, addr, lockedCoins, start.Unix(), end.Unix()); err != nil {
			return fmt.Errorf("bucket %s: %w", a.Name, err)
		}
		logger.Info("continuous vesting configured",
			"bucket", a.Name, "address", a.Address, "locked", lockedCoins.String(),
			"start", start.Format(time.RFC3339), "end", end.Format(time.RFC3339))

	case config.VestPeriodicQuarterly:
		periods, err := config.BuildQuarterlyPeriods(now, a.CliffMonths, a.DurationMonths, a.FirstUnlockAtCliffEnd, locked)
		if err != nil {
			return fmt.Errorf("bucket %s: %w", a.Name, err)
		}
		lockedCoins := sdk.NewCoins(sdk.NewCoin(config.BondDenom, locked))
		if err := setPeriodicVesting(ctx, k.AccountKeeper, addr, lockedCoins, now.Unix(), periods); err != nil {
			return fmt.Errorf("bucket %s: %w", a.Name, err)
		}
		logger.Info("periodic vesting configured",
			"bucket", a.Name, "address", a.Address, "locked", lockedCoins.String(),
			"periods", fmt.Sprintf("%d", len(periods)))

	default:
		return fmt.Errorf("bucket %s: unknown vest kind %d", a.Name, a.Kind)
	}

	coins := sdk.NewCoins(sdk.NewCoin(config.BondDenom, total))
	if err := k.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, addr, coins); err != nil {
		return fmt.Errorf("funding bucket %s: %w", a.Name, err)
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent("community_continuance_allocation",
		sdk.NewAttribute("bucket", a.Name),
		sdk.NewAttribute("address", a.Address),
		sdk.NewAttribute("total", total.String()+config.BondDenom),
		sdk.NewAttribute("locked", locked.String()+config.BondDenom),
	))
	return nil
}

// executeValidatorProgram pays the eligible seats and locks the remainder.
//
// Per v23 and the locked decisions:
//   - 10% (7.2M) is the seat pool: 30 seats x 240,000 SCRT.
//   - A seat is paid ONLY if the validator supplied a payout address AND the
//     operator is still bonded and not jailed at this height.
//   - Anything else — vacant, unknown operator, unbonded, jailed, or a failed
//     send — degrades to a vacant seat. Its 240,000 SCRT stays liquid at the
//     program address so it can be paid manually later. v23: "a vacant seat's
//     funds wait for a validator." Seat data can never halt the chain.
//   - The remaining 90% (64.8M) is locked at the program address in a
//     PeriodicVestingAccount unlocking quarterly over 5 years.
func executeValidatorProgram(ctx sdk.Context, k *keepers.SecretAppKeepers, logger log.Logger, now time.Time, n config.Network) error {
	vp := n.ValidatorProgram()

	programAddr, err := sdk.AccAddressFromBech32(vp.ProgramAddress)
	if err != nil {
		return fmt.Errorf("program address: %w", err) // unreachable; Validate ran
	}

	seatAmount := config.ToUscrt(config.ReservedSeatSCRT)
	paidTotal := sdkmath.ZeroInt()
	paidCount := 0

	for i, seat := range n.Seats {
		slot := fmt.Sprintf("%02d", i+1)

		if !seat.Filled() {
			logger.Info("validator seat vacant — reserve waits at program address", "slot", slot)
			continue
		}

		reason := seatIneligibleReason(ctx, k, seat)
		if reason != "" {
			logger.Warn("validator seat skipped — reserve waits at program address",
				"slot", slot, "operator", seat.Operator, "reason", reason)
			ctx.EventManager().EmitEvent(sdk.NewEvent("community_continuance_validator_seat_skipped",
				sdk.NewAttribute("slot", slot),
				sdk.NewAttribute("operator", seat.Operator),
				sdk.NewAttribute("reason", reason),
			))
			continue
		}

		to, err := sdk.AccAddressFromBech32(seat.Address)
		if err != nil {
			logger.Error("validator seat payout address invalid — treating as vacant",
				"slot", slot, "operator", seat.Operator, "error", err.Error())
			continue
		}

		coins := sdk.NewCoins(sdk.NewCoin(config.BondDenom, seatAmount))
		if err := k.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, to, coins); err != nil {
			// Deliberately not fatal: one bad payout address must not halt
			// the chain. The reserve simply stays at the program address.
			logger.Error("validator seat payout failed — treating as vacant",
				"slot", slot, "operator", seat.Operator, "address", seat.Address, "error", err.Error())
			ctx.EventManager().EmitEvent(sdk.NewEvent("community_continuance_validator_seat_skipped",
				sdk.NewAttribute("slot", slot),
				sdk.NewAttribute("operator", seat.Operator),
				sdk.NewAttribute("reason", "payout_send_failed"),
			))
			continue
		}

		paidTotal = paidTotal.Add(seatAmount)
		paidCount++
		logger.Info("validator seat paid",
			"slot", slot, "operator", seat.Operator, "address", seat.Address, "amount", coins.String())
		ctx.EventManager().EmitEvent(sdk.NewEvent("community_continuance_validator_seat_paid",
			sdk.NewAttribute("slot", slot),
			sdk.NewAttribute("operator", seat.Operator),
			sdk.NewAttribute("address", seat.Address),
			sdk.NewAttribute("amount", coins.String()),
		))
	}

	// The 90% tranche is locked at the program address. The unclaimed seat
	// reserves land there too, and stay liquid: original_vesting is the 90%
	// only, so spendable == unclaimed reserves.
	locked := config.ToUscrt(vp.VestingSCRT())
	periods, err := config.BuildQuarterlyPeriods(now, 0, vp.VestDurationMonths, false, locked)
	if err != nil {
		return fmt.Errorf("building program vesting schedule: %w", err)
	}
	lockedCoins := sdk.NewCoins(sdk.NewCoin(config.BondDenom, locked))
	if err := setPeriodicVesting(ctx, k.AccountKeeper, programAddr, lockedCoins, now.Unix(), periods); err != nil {
		return fmt.Errorf("program address vesting: %w", err)
	}

	remainder := config.ToUscrt(vp.TotalSCRT).Sub(paidTotal)
	if remainder.LT(locked) {
		// Unreachable: paidTotal <= upfront pool = total - locked. Checked
		// anyway, because funding below the locked amount would leave the
		// account permanently unable to satisfy its own schedule.
		return fmt.Errorf("program funding %s is below its locked amount %s", remainder, locked)
	}
	remainderCoins := sdk.NewCoins(sdk.NewCoin(config.BondDenom, remainder))
	if err := k.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, programAddr, remainderCoins); err != nil {
		return fmt.Errorf("funding program address: %w", err)
	}

	unclaimed := remainder.Sub(locked)
	logger.Info("validator program funded",
		"program_address", vp.ProgramAddress,
		"seats_paid", fmt.Sprintf("%d", paidCount),
		"seats_total", fmt.Sprintf("%d", config.ValidatorSeatCount),
		"paid_out", paidTotal.String()+config.BondDenom,
		"program_funded", remainderCoins.String(),
		"locked_quarterly_5y", lockedCoins.String(),
		"unclaimed_reserves_liquid", unclaimed.String()+config.BondDenom)

	ctx.EventManager().EmitEvent(sdk.NewEvent("community_continuance_validator_program",
		sdk.NewAttribute("program_address", vp.ProgramAddress),
		sdk.NewAttribute("seats_paid", fmt.Sprintf("%d", paidCount)),
		sdk.NewAttribute("paid_out", paidTotal.String()+config.BondDenom),
		sdk.NewAttribute("program_funded", remainder.String()+config.BondDenom),
		sdk.NewAttribute("locked", locked.String()+config.BondDenom),
		sdk.NewAttribute("unclaimed_reserves", unclaimed.String()+config.BondDenom),
	))
	return nil
}

// seatIneligibleReason returns "" if the seat should be paid, or a short
// machine-readable reason if it must be treated as vacant. It never returns an
// error: no validator's state can halt the upgrade.
func seatIneligibleReason(ctx sdk.Context, k *keepers.SecretAppKeepers, seat config.ValidatorSeat) string {
	valAddr, err := sdk.ValAddressFromBech32(seat.Operator)
	if err != nil {
		return "operator_address_invalid"
	}
	validator, err := k.StakingKeeper.GetValidator(ctx, valAddr)
	if err != nil {
		return "validator_not_found"
	}
	if validator.IsJailed() {
		return "validator_jailed"
	}
	if !validator.IsBonded() {
		return "validator_not_bonded"
	}
	return ""
}

// ---------------------------------------------------------------------------
// Vesting account setup
// ---------------------------------------------------------------------------

// requireSeededBaseAccount refuses an address that does not already hold a
// plain BaseAccount. Custody buckets must be pre-seeded; this never creates
// accounts. Seat payout addresses are not gated here — a bad seat degrades
// to vacant rather than halting the chain.
func requireSeededBaseAccount(ctx sdk.Context, ak *authkeeper.AccountKeeper, addr sdk.AccAddress, what string) error {
	acc := ak.GetAccount(ctx, addr)
	if acc == nil {
		return fmt.Errorf("%s: address %s has NO ACCOUNT on chain — it was never seeded. "+
			"Funding it would create a fresh account at an address nobody may hold. "+
			"Seed every custody address with 1 uscrt and verify it before publication (CHECKLIST T6a / M1s)", what, addr)
	}
	if _, ok := acc.(*authtypes.BaseAccount); !ok {
		return fmt.Errorf("%s: address %s already exists with type %T, not a plain BaseAccount; refusing to fund it", what, addr, acc)
	}
	return nil
}

func ensureBaseAccount(ctx sdk.Context, ak *authkeeper.AccountKeeper, addr sdk.AccAddress) (*authtypes.BaseAccount, error) {
	acc := ak.GetAccount(ctx, addr)
	if acc == nil {
		fresh := ak.NewAccountWithAddress(ctx, addr)
		base, ok := fresh.(*authtypes.BaseAccount)
		if !ok {
			return nil, fmt.Errorf("new account for %s has unexpected type %T", addr, fresh)
		}
		return base, nil
	}
	base, ok := acc.(*authtypes.BaseAccount)
	if !ok {
		return nil, fmt.Errorf("account %s already exists with type %T; refusing to overwrite it — was this address seeded before publication?", addr, acc)
	}
	return base, nil
}

func setContinuousVesting(ctx sdk.Context, ak *authkeeper.AccountKeeper, addr sdk.AccAddress, locked sdk.Coins, start, end int64) error {
	base, err := ensureBaseAccount(ctx, ak, addr)
	if err != nil {
		return err
	}
	acct, err := vestingtypes.NewContinuousVestingAccount(base, locked, start, end)
	if err != nil {
		return fmt.Errorf("creating continuous vesting account for %s: %w", addr, err)
	}
	ak.SetAccount(ctx, acct)
	return nil
}

func setPeriodicVesting(ctx sdk.Context, ak *authkeeper.AccountKeeper, addr sdk.AccAddress, locked sdk.Coins, start int64, periods vestingtypes.Periods) error {
	base, err := ensureBaseAccount(ctx, ak, addr)
	if err != nil {
		return err
	}
	acct, err := vestingtypes.NewPeriodicVestingAccount(base, locked, start, periods)
	if err != nil {
		return fmt.Errorf("creating periodic vesting account for %s: %w", addr, err)
	}
	ak.SetAccount(ctx, acct)
	return nil
}

// ---------------------------------------------------------------------------
// Monetary policy
// ---------------------------------------------------------------------------

func pinInflation(ctx sdk.Context, k *keepers.SecretAppKeepers) error {
	target, err := sdkmath.LegacyNewDecFromStr(config.TargetInflation)
	if err != nil {
		return fmt.Errorf("parsing target inflation: %w", err)
	}

	params, err := k.MintKeeper.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("reading mint params: %w", err)
	}
	params.InflationMin = target
	params.InflationMax = target
	params.InflationRateChange = sdkmath.LegacyZeroDec()
	if err := params.Validate(); err != nil {
		return fmt.Errorf("mint params invalid after change: %w", err)
	}
	if err := k.MintKeeper.Params.Set(ctx, params); err != nil {
		return fmt.Errorf("writing mint params: %w", err)
	}

	minter, err := k.MintKeeper.Minter.Get(ctx)
	if err != nil {
		return fmt.Errorf("reading minter: %w", err)
	}
	minter.Inflation = target
	if err := k.MintKeeper.Minter.Set(ctx, minter); err != nil {
		return fmt.Errorf("writing minter: %w", err)
	}
	return nil
}

func zeroFoundationTax(ctx sdk.Context, k *keepers.SecretAppKeepers, logger log.Logger) error {
	dp, err := k.DistrKeeper.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("reading distribution params: %w", err)
	}
	dp.SecretFoundationTax = sdkmath.LegacyZeroDec()
	// Safe to clear: this fork's AllocateTokens applies the tax only when the
	// rate is nonzero AND the address is non-empty, and params validation
	// accepts an empty address. The community tax is deliberately untouched.
	dp.SecretFoundationAddress = ""
	if err := k.DistrKeeper.Params.Set(ctx, dp); err != nil {
		return fmt.Errorf("writing distribution params: %w", err)
	}
	logger.Info("secret foundation tax set to 0 and foundation address cleared")
	return nil
}
