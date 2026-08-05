package config

import (
	"fmt"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"os"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// The handler runs inside secretd, where bech32 prefixes are set at app init.
// A bare `go test` process must set them itself.
func TestMain(m *testing.M) {
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount("secret", "secretpub")
	cfg.SetBech32PrefixForValidator("secretvaloper", "secretvaloperpub")
	cfg.SetBech32PrefixForConsensusNode("secretvalcons", "secretvalconspub")
	os.Exit(m.Run())
}

// testAddr builds a deterministic, valid secret1 address.
func testAddr(seed byte) string {
	b := make([]byte, 20)
	for i := range b {
		b[i] = seed
	}
	return sdk.AccAddress(b).String()
}

// testValoper builds a deterministic, valid secretvaloper address.
func testValoper(seed byte) string {
	b := make([]byte, 20)
	for i := range b {
		b[i] = seed
	}
	return sdk.ValAddress(b).String()
}

// fixture is a fully-populated network with valid throwaway addresses. It lets
// the economics, seat math and schedule logic be tested to completion without
// waiting for real custody addresses to exist.
func fixture() Network {
	return Network{
		Name:    "fixture",
		ChainID: "fixture-1",
		Addresses: AddressSet{
			Foundation:            testAddr(1),
			CoreDevelopment:       testAddr(2),
			Advisors:              testAddr(3),
			EcosystemFund:         testAddr(4),
			ResearchDevelopment:   testAddr(5),
			BuilderRelayerSupport: testAddr(6),
			Remediation:           testAddr(7),
			ValidatorProgram:      testAddr(8),
		},
		Seats: openSeats(),
	}
}

// ---------------------------------------------------------------------------
// Economics
// ---------------------------------------------------------------------------

// TestAllocationTableSumsToAnnouncedTotal is the check that stops a mistyped
// bucket size from minting a different supply than the proposal promises.
func TestAllocationTableSumsToAnnouncedTotal(t *testing.T) {
	n := fixture()
	var sum int64
	for _, a := range n.Allocations() {
		sum += a.TotalSCRT
	}
	sum += n.ValidatorProgram().TotalSCRT

	if sum != TotalMintSCRT {
		t.Fatalf("allocation table sums to %d SCRT, want %d", sum, TotalMintSCRT)
	}
	if TotalMintSCRT != 1_079_000_000 {
		t.Fatalf("TotalMintSCRT is %d, want 1079000000 — the proposal's committed mint", TotalMintSCRT)
	}
}

// TestBucketShapesMatchProposal pins every bucket against Draft Proposal v23.
func TestBucketShapesMatchProposal(t *testing.T) {
	want := map[string]struct {
		total    int64
		liquid   int64
		kind     VestKind
		cliff    int
		duration int
	}{
		"foundation":              {299_000_000, 10, VestContinuous, 6, 60},
		"core_development":        {299_000_000, 10, VestContinuous, 6, 60},
		"advisors":                {72_000_000, 0, VestPeriodicQuarterly, 6, 48},
		"ecosystem_fund":          {178_000_000, 100, VestNone, 0, 0},
		"research_development":    {72_000_000, 10, VestContinuous, 0, 60},
		"builder_relayer_support": {43_000_000, 100, VestNone, 0, 0},
		"remediation":             {44_000_000, 30, VestContinuous, 6, 24},
	}

	got := fixture().Allocations()
	if len(got) != len(want) {
		t.Fatalf("got %d buckets, want %d", len(got), len(want))
	}
	for _, a := range got {
		w, ok := want[a.Name]
		if !ok {
			t.Fatalf("unexpected bucket %q", a.Name)
		}
		if a.TotalSCRT != w.total || a.LiquidPct != w.liquid || a.Kind != w.kind ||
			a.CliffMonths != w.cliff || a.DurationMonths != w.duration {
			t.Errorf("bucket %s = {total %d, liquid %d%%, kind %d, cliff %d, duration %d}; want {%d, %d%%, %d, %d, %d}",
				a.Name, a.TotalSCRT, a.LiquidPct, a.Kind, a.CliffMonths, a.DurationMonths,
				w.total, w.liquid, w.kind, w.cliff, w.duration)
		}
	}
}

// TestDayOneLiquidMatchesProposal checks the total immediately-spendable amount
// against v24's own stated figure of "about 308M SCRT (~21.4%)".
//
// This is the check that caught the validator program shipping 100% liquid
// (it overshot by exactly 64.8M), and the one that confirmed v24's move of
// Builder & Relayer Support to fully liquid (+38.7M over v23's 269.7M).
func TestDayOneLiquidMatchesProposal(t *testing.T) {
	n := fixture()
	liquid := sdkmath.ZeroInt()
	for _, a := range n.Allocations() {
		liquid = liquid.Add(a.Liquid())
	}
	// Only the seat pool is liquid from the validator bucket; the 90% is locked.
	liquid = liquid.Add(ToUscrt(n.ValidatorProgram().UpfrontPoolSCRT()))

	wantSCRT := int64(308_400_000)
	if got := liquid.QuoRaw(MicroUnitsPerSCRT).Int64(); got != wantSCRT {
		t.Fatalf("day-one liquid = %d SCRT, want %d (v24: \"about 308M\", ~21.4%%)", got, wantSCRT)
	}
}

// ---------------------------------------------------------------------------
// Validator program
// ---------------------------------------------------------------------------

func TestSeatMath(t *testing.T) {
	vp := fixture().ValidatorProgram()

	if vp.TotalSCRT != 72_000_000 {
		t.Fatalf("validator bucket = %d, want 72000000", vp.TotalSCRT)
	}
	if pool := vp.UpfrontPoolSCRT(); pool != 7_200_000 {
		t.Fatalf("upfront pool = %d, want 7200000", pool)
	}
	if vesting := vp.VestingSCRT(); vesting != 64_800_000 {
		t.Fatalf("vesting tranche = %d, want 64800000", vesting)
	}
	if vp.UpfrontPoolSCRT()%int64(ValidatorSeatCount) != 0 {
		t.Fatalf("pool does not divide evenly across %d seats", ValidatorSeatCount)
	}
	if per := vp.UpfrontPoolSCRT() / int64(ValidatorSeatCount); per != ReservedSeatSCRT {
		t.Fatalf("per-seat = %d, want ReservedSeatSCRT %d", per, ReservedSeatSCRT)
	}
	if vp.UpfrontPoolSCRT()+vp.VestingSCRT() != vp.TotalSCRT {
		t.Fatal("pool + vesting must equal the bucket total")
	}
}

// TestUnclaimedSeatsStayLiquidAtProgram proves the funding arithmetic the
// handler relies on: however many seats go unpaid, the program address ends up
// with exactly the locked tranche plus the unclaimed reserves, and the
// unclaimed portion is spendable (balance minus original_vesting).
func TestUnclaimedSeatsStayLiquidAtProgram(t *testing.T) {
	vp := fixture().ValidatorProgram()
	total := ToUscrt(vp.TotalSCRT)
	locked := ToUscrt(vp.VestingSCRT())
	seat := ToUscrt(ReservedSeatSCRT)

	for paidCount := 0; paidCount <= ValidatorSeatCount; paidCount++ {
		paid := seat.MulRaw(int64(paidCount))
		funded := total.Sub(paid)

		if funded.LT(locked) {
			t.Fatalf("paidCount=%d: program funding %s < locked %s", paidCount, funded, locked)
		}
		spendable := funded.Sub(locked)
		wantSpendable := seat.MulRaw(int64(ValidatorSeatCount - paidCount))
		if !spendable.Equal(wantSpendable) {
			t.Fatalf("paidCount=%d: spendable %s, want %s", paidCount, spendable, wantSpendable)
		}
		if !paid.Add(funded).Equal(total) {
			t.Fatalf("paidCount=%d: paid + funded != bucket total", paidCount)
		}
	}
}

// ---------------------------------------------------------------------------
// Schedules
// ---------------------------------------------------------------------------

// TestSchedulesBuildAtEveryProbeTime is the gap that let a chain-halting
// schedule pass the old release gate: validation never constructed one.
func TestSchedulesBuildAtEveryProbeTime(t *testing.T) {
	n := fixture()
	for _, at := range scheduleProbeTimes() {
		if err := dryRunSchedules(n, at); err != nil {
			t.Fatalf("block time %s: %v", at.Format(time.RFC3339), err)
		}
	}
}

func TestAdvisorsSchedule(t *testing.T) {
	start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

	var advisors Allocation
	for _, a := range fixture().Allocations() {
		if a.Name == "advisors" {
			advisors = a
		}
	}

	periods, err := BuildQuarterlyPeriods(start, advisors.CliffMonths, advisors.DurationMonths, advisors.FirstUnlockAtCliffEnd, advisors.Locked())
	if err != nil {
		t.Fatalf("building advisors schedule: %v", err)
	}
	if len(periods) != 16 {
		t.Fatalf("got %d periods, want 16 (v24: quarterly over 4 years)", len(periods))
	}

	// Equal amounts of 4.5M SCRT each (72M / 16), summing to the locked total.
	wantEach := ToUscrt(4_500_000)
	sum := sdkmath.ZeroInt()
	for i, p := range periods {
		amt := p.Amount.AmountOf(BondDenom)
		if !amt.Equal(wantEach) {
			t.Errorf("period %d amount = %s, want %s", i, amt, wantEach)
		}
		sum = sum.Add(amt)
	}
	if !sum.Equal(advisors.Locked()) {
		t.Fatalf("periods sum to %s, want %s", sum, advisors.Locked())
	}

	// First unlock lands 9 months out (6-month cliff + one quarter), so
	// period 0 is much longer than a quarter. This is the property that must
	// never be described as "20 equal quarterly periods".
	firstUnlock := start.Add(time.Duration(periods[0].Length) * time.Second)
	wantFirst := start.AddDate(0, 9, 0)
	if !firstUnlock.Equal(wantFirst) {
		t.Fatalf("first unlock at %s, want %s", firstUnlock, wantFirst)
	}
	if periods[0].Length <= periods[1].Length {
		t.Fatal("period 0 should absorb the cliff and be longer than a normal quarter")
	}

	// Last unlock at month 54 (cliff 6 + 48).
	last := start
	for _, p := range periods {
		last = last.Add(time.Duration(p.Length) * time.Second)
	}
	if want := start.AddDate(0, 54, 0); !last.Equal(want) {
		t.Fatalf("final unlock at %s, want %s", last, want)
	}
}

func TestValidatorProgramSchedule(t *testing.T) {
	start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	vp := fixture().ValidatorProgram()
	locked := ToUscrt(vp.VestingSCRT())

	periods, err := BuildQuarterlyPeriods(start, 0, vp.VestDurationMonths, false, locked)
	if err != nil {
		t.Fatalf("building program schedule: %v", err)
	}
	if len(periods) != 20 {
		t.Fatalf("got %d periods, want 20 (v24: validator program is quarterly over 5 years)", len(periods))
	}

	// No cliff: first unlock one quarter out, last at 5 years.
	first := start.Add(time.Duration(periods[0].Length) * time.Second)
	if want := start.AddDate(0, 3, 0); !first.Equal(want) {
		t.Fatalf("first unlock at %s, want %s", first, want)
	}
	sum := sdkmath.ZeroInt()
	last := start
	for _, p := range periods {
		sum = sum.Add(p.Amount.AmountOf(BondDenom))
		last = last.Add(time.Duration(p.Length) * time.Second)
	}
	if !sum.Equal(locked) {
		t.Fatalf("periods sum to %s, want %s", sum, locked)
	}
	if want := start.AddDate(0, 60, 0); !last.Equal(want) {
		t.Fatalf("final unlock at %s, want %s", last, want)
	}
}

// TestQuarterlyPeriodsRejectImpossibleSchedule is the exact configuration that
// previously passed the release gate green and then halted the chain.
func TestQuarterlyPeriodsRejectImpossibleSchedule(t *testing.T) {
	start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	_, err := BuildQuarterlyPeriods(start, 0, 60, true, ToUscrt(1_000_000))
	if err == nil {
		t.Fatal("expected an error: cliff 0 with firstAtCliffEnd=true makes period 0 zero-length")
	}
}

func TestContinuousWindowHonoursCliff(t *testing.T) {
	start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	a := Allocation{Name: "x", TotalSCRT: 100, LiquidPct: 10, Kind: VestContinuous, CliffMonths: 6, DurationMonths: 60}

	s, e := ContinuousWindow(start, a)
	if want := start.AddDate(0, 6, 0); !s.Equal(want) {
		t.Fatalf("vesting starts %s, want %s", s, want)
	}
	if want := start.AddDate(0, 66, 0); !e.Equal(want) {
		t.Fatalf("vesting ends %s, want %s", e, want)
	}
	if !e.After(s) {
		t.Fatal("end must be after start")
	}
}

// ---------------------------------------------------------------------------
// Validation rejects bad configuration
// ---------------------------------------------------------------------------

func TestValidateAcceptsFixture(t *testing.T) {
	if err := Validate(fixture()); err != nil {
		t.Fatalf("fixture should validate: %v", err)
	}
}

func TestValidateRejectsPlaceholders(t *testing.T) {
	n := fixture()
	n.Addresses.Foundation = FillMe
	if err := Validate(n); err == nil {
		t.Fatal("expected a placeholder address to be rejected")
	}
}

// TestValidateRejectsWrongLengthAddresses: account addresses must be 20 bytes
// after bech32 decode (SDK default verifier alone is weaker than that).
func TestValidateRejectsWrongLengthAddresses(t *testing.T) {
	shortAddr := sdk.AccAddress([]byte{7}).String()
	if _, err := sdk.AccAddressFromBech32(shortAddr); err != nil {
		t.Fatalf("test premise broken: %q should still decode: %v", shortAddr, err)
	}

	n := fixture()
	n.Addresses.Remediation = shortAddr
	if err := Validate(n); err == nil {
		t.Fatalf("expected a %d-byte address (%s) to be rejected", 1, shortAddr)
	}

	// 32 bytes decodes too, and is equally not a normal account.
	long := make([]byte, 32)
	for i := range long {
		long[i] = 9
	}
	n = fixture()
	n.Addresses.Advisors = sdk.AccAddress(long).String()
	if err := Validate(n); err == nil {
		t.Fatal("expected a 32-byte address to be rejected")
	}

	// A seat payout address gets the same treatment.
	n = fixture()
	n.Seats[0] = ValidatorSeat{Operator: testValoper(60), Address: shortAddr}
	if err := Validate(n); err == nil {
		t.Fatal("expected a short seat payout address to be rejected")
	}

	// And so does the OPERATOR — same hole, other field. A malformed operator
	// would not misdirect money (the seat is skipped) but would silently deny a
	// real validator their payout.
	shortVal := sdk.ValAddress([]byte{5}).String()
	if _, err := sdk.ValAddressFromBech32(shortVal); err != nil {
		t.Fatalf("test premise broken: %q should still decode: %v", shortVal, err)
	}
	n = fixture()
	n.Seats[0] = ValidatorSeat{Operator: shortVal, Address: testAddr(61)}
	if err := Validate(n); err == nil {
		t.Fatalf("expected a %d-byte operator (%s) to be rejected", 1, shortVal)
	}
}

func TestValidateRejectsDuplicateAddresses(t *testing.T) {
	n := fixture()
	n.Addresses.CoreDevelopment = n.Addresses.Foundation
	if err := Validate(n); err == nil {
		t.Fatal("expected duplicate bucket addresses to be rejected")
	}
}

// TestValidateRejectsCaseVariantDuplicates guards a real defect: bech32 is
// case-insensitive, so an address pasted in upper case is the SAME account as
// its lower-case form. Keying the dedup map on the raw string let such a pair
// pass validation and then halt the chain at the upgrade height — after the
// mint — when the second bucket hit the vesting account the first had created.
func TestValidateRejectsCaseVariantDuplicates(t *testing.T) {
	n := fixture()
	n.Addresses.CoreDevelopment = strings.ToUpper(n.Addresses.Foundation)

	// Sanity: the two spellings really are the same account, and both decode.
	lower, err := sdk.AccAddressFromBech32(n.Addresses.Foundation)
	if err != nil {
		t.Fatalf("lower-case address should decode: %v", err)
	}
	upper, err := sdk.AccAddressFromBech32(n.Addresses.CoreDevelopment)
	if err != nil {
		t.Fatalf("upper-case bech32 should decode to the same account: %v", err)
	}
	if !lower.Equals(upper) {
		t.Fatal("test premise broken: the two spellings decode to different accounts")
	}

	if err := Validate(n); err == nil {
		t.Fatal("expected a case-variant duplicate address to be rejected")
	}

	// The same must hold for a seat reusing a bucket address in another case.
	n = fixture()
	n.Seats[0] = ValidatorSeat{Operator: testValoper(50), Address: strings.ToUpper(n.Addresses.EcosystemFund)}
	if err := Validate(n); err == nil {
		t.Fatal("expected a seat reusing a bucket address in upper case to be rejected")
	}
}

// TestValidateRejectsDuplicateOperators guards the same defect class as
// duplicate addresses, on the other field: two seats naming the SAME validator
// would pay it 480,000 SCRT across two payout addresses while starving a seat
// that should have gone to somebody else. Nothing at execution would catch it —
// both sends succeed.
func TestValidateRejectsDuplicateOperators(t *testing.T) {
	op := testValoper(90)

	n := fixture()
	n.Seats[0] = ValidatorSeat{Operator: op, Address: testAddr(90)}
	n.Seats[1] = ValidatorSeat{Operator: op, Address: testAddr(91)} // same validator, different payout
	if err := Validate(n); err == nil {
		t.Fatal("expected two seats naming the same validator to be rejected")
	}

	// Case variants of one operator are the same validator too.
	n = fixture()
	n.Seats[0] = ValidatorSeat{Operator: op, Address: testAddr(92)}
	n.Seats[1] = ValidatorSeat{Operator: strings.ToUpper(op), Address: testAddr(93)}
	if err := Validate(n); err == nil {
		t.Fatal("expected a case-variant duplicate operator to be rejected")
	}

	// Distinct validators with distinct payout addresses must still pass.
	n = fixture()
	n.Seats[0] = ValidatorSeat{Operator: testValoper(94), Address: testAddr(94)}
	n.Seats[1] = ValidatorSeat{Operator: testValoper(95), Address: testAddr(95)}
	if err := Validate(n); err != nil {
		t.Fatalf("two distinct validators should validate: %v", err)
	}
}

func TestValidateRejectsSeatSharingBucketAddress(t *testing.T) {
	n := fixture()
	n.Seats[0] = ValidatorSeat{Operator: testValoper(50), Address: n.Addresses.Foundation}
	if err := Validate(n); err == nil {
		t.Fatal("expected a seat reusing a bucket address to be rejected")
	}
}

func TestValidateRejectsDuplicateSeatAddresses(t *testing.T) {
	n := fixture()
	shared := testAddr(60)
	n.Seats[0] = ValidatorSeat{Operator: testValoper(50), Address: shared}
	n.Seats[1] = ValidatorSeat{Operator: testValoper(51), Address: shared}
	if err := Validate(n); err == nil {
		t.Fatal("expected two seats sharing a payout address to be rejected")
	}
}

func TestValidateRejectsPartialSeat(t *testing.T) {
	n := fixture()
	n.Seats[3] = ValidatorSeat{Operator: testValoper(70)} // no payout address
	if err := Validate(n); err == nil {
		t.Fatal("expected a half-filled seat to be rejected")
	}

	n = fixture()
	n.Seats[3] = ValidatorSeat{Address: testAddr(71)} // no operator
	if err := Validate(n); err == nil {
		t.Fatal("expected a half-filled seat to be rejected")
	}
}

func TestValidateRejectsBadOperator(t *testing.T) {
	n := fixture()
	n.Seats[5] = ValidatorSeat{Operator: "secret1notavaloper", Address: testAddr(80)}
	if err := Validate(n); err == nil {
		t.Fatal("expected an invalid secretvaloper operator to be rejected")
	}
}

func TestValidateRejectsWrongSeatCount(t *testing.T) {
	n := fixture()
	n.Seats = n.Seats[:ValidatorSeatCount-1]
	if err := Validate(n); err == nil {
		t.Fatalf("expected exactly %d seats to be required", ValidatorSeatCount)
	}
}

func TestValidateAcceptsFullyFilledSeats(t *testing.T) {
	n := fixture()
	for i := range n.Seats {
		n.Seats[i] = ValidatorSeat{Operator: testValoper(byte(100 + i)), Address: testAddr(byte(150 + i))}
	}
	if err := Validate(n); err != nil {
		t.Fatalf("a fully filled seat registry should validate: %v", err)
	}
}

func TestValidateRejectsMissingChainID(t *testing.T) {
	n := fixture()
	n.ChainID = ""
	if err := Validate(n); err == nil {
		t.Fatal("expected a missing chain ID to be rejected")
	}
}

// ---------------------------------------------------------------------------
// Network identity
// ---------------------------------------------------------------------------

// TestNetworksAreDistinguishable is the second half of the network guard: even
// if a binary reached the wrong chain, its upgrade name would not match that
// chain's governance plan.
func TestNetworksAreDistinguishable(t *testing.T) {
	seenChain := map[string]string{}
	for _, n := range Networks {
		if prev, dup := seenChain[n.ChainID]; dup {
			t.Fatalf("%s and %s share chain ID %q — ForChainID could not tell them apart", n.Name, prev, n.ChainID)
		}
		seenChain[n.ChainID] = n.Name
	}
	if Mainnet.ChainID != "secret-4" {
		t.Fatalf("mainnet chain ID = %q, want secret-4", Mainnet.ChainID)
	}
	if LocalSecret.ChainID != "secretdev-1" {
		t.Fatalf("localsecret chain ID = %q, want secretdev-1 (what LocalSecret actually runs)", LocalSecret.ChainID)
	}
	if UpgradeName != "v1.26.0-community-continuance" {
		t.Fatalf("UpgradeName = %q — it must match the governance plan byte-for-byte", UpgradeName)
	}
}

// TestForChainIDSelectsTheRightNetwork is the replacement for the old
// build-time `Active` variable. Selecting at runtime means there is no such
// thing as a wrong-network binary: the handler always uses the addresses that
// belong to the chain it is actually on, and refuses outright on any other.
func TestForChainIDSelectsTheRightNetwork(t *testing.T) {
	for _, want := range Networks {
		got, err := ForChainID(want.ChainID)
		if err != nil {
			t.Fatalf("ForChainID(%q): %v", want.ChainID, err)
		}
		if got.Name != want.Name {
			t.Fatalf("ForChainID(%q) = %q, want %q", want.ChainID, got.Name, want.Name)
		}
	}

	// An unknown chain must be a hard error, never a silent default.
	for _, bad := range []string{"", "secret-3", "cosmoshub-4", "SECRET-4", "pulsar-2"} {
		if _, err := ForChainID(bad); err == nil {
			t.Fatalf("ForChainID(%q) returned a network; unknown chains must be refused", bad)
		}
	}
}

// ---------------------------------------------------------------------------
// Release gate
// ---------------------------------------------------------------------------

// TestNetworkIsReleaseReady runs the real Validate against the network named by
// CONTINUANCE_RELEASE_GATE — the exact check the handler runs at the height.
//
// It is skipped by default so ordinary `go test ./...` stays green while
// addresses are placeholders. Before cutting ANY binary, run it for the chain
// that binary will serve:
//
//	CONTINUANCE_RELEASE_GATE=secret-4    go test ./app/upgrades/v1.26/config/ -run ReleaseReady -v
//	CONTINUANCE_RELEASE_GATE=pulsar-3    go test ./app/upgrades/v1.26/config/ -run ReleaseReady -v
//	CONTINUANCE_RELEASE_GATE=secretdev-1 go test ./app/upgrades/v1.26/config/ -run ReleaseReady -v
//
// A red result means the binary is not fit to serve that chain.
func TestNetworkIsReleaseReady(t *testing.T) {
	chainID := os.Getenv("CONTINUANCE_RELEASE_GATE")
	if chainID == "" {
		t.Skip("release gate not requested; set CONTINUANCE_RELEASE_GATE=<chain-id> before cutting a binary")
	}
	n, err := ForChainID(chainID)
	if err != nil {
		t.Fatalf("release gate: %v", err)
	}
	if err := Validate(n); err != nil {
		t.Fatalf("network %q (%s) is NOT release ready: %v", n.Name, n.ChainID, err)
	}
	filled := 0
	for _, s := range n.Seats {
		if s.Filled() {
			filled++
		}
	}
	t.Log(fmt.Sprintf("RELEASE READY: network=%s chain-id=%s upgrade=%s seats_filled=%d/%d",
		n.Name, n.ChainID, UpgradeName, filled, ValidatorSeatCount))
}

// TestFilledNetworksValidate arms itself automatically.
//
// THE GAP THIS CLOSES. Validate() is what rejects a half-filled seat, a
// duplicate operator, a wrong-length address — and the handler re-runs it at
// the upgrade height BEFORE the mint (upgrade.go), so anything it catches is a
// full-network halt with nothing committed.
//
// But nothing ran Validate() against the REAL network tables by default. It
// could not: while an AddressSet still holds FillMe placeholders, Validate fails
// on the address check and would fail for every network forever. So the only
// gate was TestNetworkIsReleaseReady, which is opt-in via an env var, and CI
// invokes it for secret-4 alone under continue-on-error.
//
// Net effect before this test: fill in Pulsar's addresses, leave one seat
// half-filled, and it passes `go test`, passes CI, and halts pulsar-3 at the
// height.
//
// This closes it without needing anyone to remember a flag. A network still on
// placeholders is skipped and logged; a network whose addresses have been filled
// in MUST validate completely. It therefore turns itself on the moment real
// addresses land, which is exactly when it starts to matter.
func TestFilledNetworksValidate(t *testing.T) {
	for _, n := range Networks {
		n := n
		t.Run(n.Name, func(t *testing.T) {
			placeholders := 0
			for _, a := range n.Allocations() {
				if a.Address == FillMe {
					placeholders++
				}
			}
			if n.Addresses.ValidatorProgram == FillMe {
				placeholders++
			}
			if placeholders > 0 {
				t.Skipf("%s (%s): %d address(es) still placeholder — this test arms itself once they are filled",
					n.Name, n.ChainID, placeholders)
			}
			if err := Validate(n); err != nil {
				t.Fatalf("%s (%s) has real addresses but does NOT validate — this would HALT the chain "+
					"at the upgrade height, because the handler re-runs Validate before the mint: %v",
					n.Name, n.ChainID, err)
			}
			filled := 0
			for _, s := range n.Seats {
				if s.Filled() {
					filled++
				}
			}
			t.Logf("%s (%s): validates, %d/%d seats filled", n.Name, n.ChainID, filled, ValidatorSeatCount)
		})
	}
}

// TestValidateRejectsModuleAccountAddresses: module accounts are never valid
// custody or seat-payout destinations.
func TestValidateRejectsModuleAccountAddresses(t *testing.T) {
	for _, name := range moduleAccountNames {
		modAddr := authtypes.NewModuleAddress(name).String()

		t.Run("bucket/"+name, func(t *testing.T) {
			n := fixture()
			n.Addresses.EcosystemFund = modAddr
			if err := Validate(n); err == nil {
				t.Fatalf("Validate accepted the %q module account as a bucket address", name)
			}
		})

		t.Run("seat/"+name, func(t *testing.T) {
			n := fixture()
			n.Seats[0] = ValidatorSeat{
				Operator: "secretvaloper1pzltl7rn7fyl3grk24m5lsk3kvpeta0tm3k6m5",
				Address:  modAddr,
			}
			if err := Validate(n); err == nil {
				t.Fatalf("Validate accepted the %q module account as a seat payout address", name)
			}
		})
	}
}

// TestModuleAccountListIsWellFormed: every moduleAccountNames entry must derive
// to a distinct 20-byte address (typos must not collapse the screen).
func TestModuleAccountListIsWellFormed(t *testing.T) {
	seen := map[string]string{}
	for _, name := range moduleAccountNames {
		addr := authtypes.NewModuleAddress(name)
		if len(addr) != sdkAddrLen {
			t.Fatalf("module %q derives to %d bytes, not %d", name, len(addr), sdkAddrLen)
		}
		if prev, dup := seen[addr.String()]; dup {
			t.Fatalf("modules %q and %q derive to the same address — one is a typo", prev, name)
		}
		seen[addr.String()] = name
	}
	if len(seen) != len(moduleAccountNames) {
		t.Fatalf("expected %d distinct module addresses, got %d", len(moduleAccountNames), len(seen))
	}
}
