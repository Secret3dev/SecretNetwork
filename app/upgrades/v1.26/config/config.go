// Package config holds the Community Continuance allocation table, the
// per-network address sets, and every rule that can be checked without a
// running chain.
//
// It deliberately imports NO app keepers. That keeps it linkable — and
// therefore testable — on any machine, including CI runners without the SGX
// toolchain. The release gate in config_test.go runs the exact same
// Validate() the upgrade handler runs at the upgrade height.
package config

import (
	"fmt"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
)

// moduleAccountNames mirrors app.ModuleAccountPermissions (app/modules.go).
// Kept as plain strings so this package stays keeper-free and linkable
// anywhere; TestModuleAccountListMatchesApp asserts it has not drifted.
//
// Why this list exists: custody and seat payout addresses must be ordinary
// user accounts under operator control. Module accounts are rejected at
// Validate() time so a mistaken paste cannot pass the release gate.
var moduleAccountNames = []string{
	"fee_collector", "distribution", "mint", "bonded_tokens_pool",
	"not_bonded_tokens_pool", "gov", "transfer", "interchainaccounts",
	"feeibc", "ibc-switch", "compute", "cron",
}

// isModuleAddress reports whether addr is one of the chain's module accounts.
// authtypes.NewModuleAddress is a pure function — no keeper, no chain state.
func isModuleAddress(addr sdk.AccAddress) (string, bool) {
	for _, name := range moduleAccountNames {
		if authtypes.NewModuleAddress(name).Equals(addr) {
			return name, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Constants — the numbers the proposal commits to
// ---------------------------------------------------------------------------

const (
	// MicroUnitsPerSCRT converts whole SCRT to uscrt (6 decimals).
	MicroUnitsPerSCRT = 1_000_000

	// BondDenom is the staking/mint denom on Secret Network.
	BondDenom = "uscrt"

	// TotalMintSCRT is the exact, announced size of the one-time mint.
	// Validate() asserts the bucket table sums to this, so a fat-fingered
	// bucket size fails the build instead of minting the wrong supply.
	TotalMintSCRT int64 = 1_079_000_000

	// ValidatorSeatCount is the fixed number of reserved program seats.
	// Seat value does NOT redistribute if a seat stays empty.
	ValidatorSeatCount = 30

	// ReservedSeatSCRT is the equal per-seat upfront share in whole SCRT.
	// 72,000,000 * 10% / 30 = 240,000. Validate() re-derives and checks this.
	ReservedSeatSCRT int64 = 240_000

	// UpgradeName is the on-chain upgrade name, IDENTICAL on every chain.
	// It is a constant, not derived from a build-time network selection, so a
	// binary's on-chain identity cannot be wrong. Must equal the governance
	// plan name byte-for-byte or every node halts at the height.
	UpgradeName = "v1.26.0-community-continuance"

	// TargetInflation pins ongoing inflation at 5%.
	TargetInflation = "0.050000000000000000"

	// ZeroFoundationTax sets the Secret-specific secret_foundation_tax to 0.
	ZeroFoundationTax = true

	// FillMe is an intentionally invalid bech32 placeholder. Validate() fails
	// loudly until every address is replaced, so a placeholder binary cannot
	// silently reach an upgrade height.
	FillMe = "secret1__FILL_ME_IN__"

	// sdkAddrLen is the byte length of a normal secp256k1 account address.
	sdkAddrLen = 20
)

// ---------------------------------------------------------------------------
// Vesting shapes
// ---------------------------------------------------------------------------

// VestKind selects the on-chain vesting mechanism for a bucket.
type VestKind int

const (
	// VestNone: fully liquid, plain bank send.
	VestNone VestKind = iota

	// VestContinuous: LiquidPct spendable at execution; the remainder unlocks
	// linearly between (execution + CliffMonths) and
	// (execution + CliffMonths + DurationMonths). Nothing from the locked
	// portion is spendable before the start time — exactly a cliff.
	VestContinuous

	// VestPeriodicQuarterly: LiquidPct spendable at execution; the remainder
	// unlocks in equal quarterly steps over DurationMonths, beginning after
	// CliffMonths.
	VestPeriodicQuarterly
)

// Allocation is one bucket of the proposal table.
type Allocation struct {
	Name    string
	Address string
	// TotalSCRT is the full bucket size in whole SCRT (liquid + locked).
	TotalSCRT int64
	// LiquidPct of TotalSCRT is spendable immediately (0–100).
	LiquidPct int64
	Kind      VestKind
	// CliffMonths delays the start of vesting (0 = starts at execution).
	CliffMonths int
	// DurationMonths of vesting after the cliff.
	DurationMonths int
	// FirstUnlockAtCliffEnd (periodic only): true → first unlock lands at the
	// cliff end; false → one quarter after it.
	FirstUnlockAtCliffEnd bool
}

// Liquid returns the immediately spendable portion in uscrt.
func (a Allocation) Liquid() sdkmath.Int {
	return ToUscrt(a.TotalSCRT).MulRaw(a.LiquidPct).QuoRaw(100)
}

// Locked returns the vesting portion in uscrt.
func (a Allocation) Locked() sdkmath.Int {
	return ToUscrt(a.TotalSCRT).Sub(a.Liquid())
}

// ---------------------------------------------------------------------------
// Validator program
// ---------------------------------------------------------------------------

// ValidatorSeat is one reserved seat in the program registry.
//
// Operator is the validator's secretvaloper — used ONLY to check, at the
// upgrade height, that the validator is still bonded and not jailed.
//
// Address is a payout address the VALIDATOR GIVES US. It is never the operator
// address and is never derived from the operator key. A seat with no address
// is vacant and its 240,000 SCRT waits at the program address.
type ValidatorSeat struct {
	Operator string
	Address  string
}

// Filled reports whether this seat is eligible for a payout at execution.
func (s ValidatorSeat) Filled() bool { return s.Operator != "" && s.Address != "" }

// Partial reports a half-edited seat (exactly one of the two fields set).
func (s ValidatorSeat) Partial() bool { return (s.Operator == "") != (s.Address == "") }

// ValidatorProgram describes the 72M bucket.
//
// At execution: eligible seats are paid ReservedSeatSCRT each, and the
// remainder goes to ProgramAddress, where the 90% tranche is locked in a
// PeriodicVestingAccount unlocking quarterly over VestDurationMonths.
type ValidatorProgram struct {
	ProgramAddress     string
	TotalSCRT          int64
	UpfrontCapPct      int64
	VestDurationMonths int
}

// UpfrontPoolSCRT is the whole seat pool (10% of the bucket).
func (v ValidatorProgram) UpfrontPoolSCRT() int64 {
	return v.TotalSCRT * v.UpfrontCapPct / 100
}

// VestingSCRT is the 90% tranche that is locked at the program address.
func (v ValidatorProgram) VestingSCRT() int64 {
	return v.TotalSCRT - v.UpfrontPoolSCRT()
}

// ---------------------------------------------------------------------------
// Networks
// ---------------------------------------------------------------------------

// AddressSet is the per-network custody address list. This is the ONLY thing
// that differs between the mainnet and testnet builds — the economics below
// are shared, so the two networks cannot drift apart.
type AddressSet struct {
	Foundation            string
	CoreDevelopment       string
	Advisors              string
	EcosystemFund         string
	ResearchDevelopment   string
	BuilderRelayerSupport string
	Remediation           string
	ValidatorProgram      string
}

// Network binds a chain ID to the address set and seat list that belong to it.
// The handler selects one at RUNTIME from ctx.ChainID(), so a single binary is
// correct on whichever chain it lands on and there is no build-time switch to
// get wrong.
type Network struct {
	Name      string
	ChainID   string
	Addresses AddressSet
	Seats     []ValidatorSeat
}

// Allocations builds the bucket table for this network. The economics live
// here, once, per Draft Proposal v24:
//
//	Foundation           299M — 10% liquid; 90% linear 5y after a 6-month cliff
//	Core Development     299M — same shape; Foundation-controlled address
//	Advisors              72M — 0% liquid; 6-month cliff, equal quarterly over 4y
//	Ecosystem Fund       178M — fully liquid
//	R&D                   72M — 10% liquid; 90% linear 5y, no cliff
//	Builder & Relayer     43M — fully liquid
//	Remediation           44M — 30% liquid; 70% linear 2y after a 6-month cliff
//
// The Validator Program's 72M is handled separately (see ValidatorProgram).
func (n Network) Allocations() []Allocation {
	a := n.Addresses
	return []Allocation{
		{
			Name: "foundation", Address: a.Foundation,
			TotalSCRT: 299_000_000, LiquidPct: 10,
			Kind: VestContinuous, CliffMonths: 6, DurationMonths: 60,
		},
		{
			// v23: "The Foundation holds this allocation and pays the core
			// development entity quarterly for services." Held at a separate
			// Foundation-controlled address so the two tranches stay
			// separately auditable for the published payout reports.
			Name: "core_development", Address: a.CoreDevelopment,
			TotalSCRT: 299_000_000, LiquidPct: 10,
			Kind: VestContinuous, CliffMonths: 6, DurationMonths: 60,
		},
		{
			// v24: "a 6-month cliff, then equal quarterly unlocks over the
			// following 4 years" — 16 unlocks of 4,500,000 SCRT at months
			// 9, 12, … 54. (v23 said 5 years / 20 unlocks of 3.6M.)
			Name: "advisors", Address: a.Advisors,
			TotalSCRT: 72_000_000, LiquidPct: 0,
			Kind: VestPeriodicQuarterly, CliffMonths: 6, DurationMonths: 48,
			FirstUnlockAtCliffEnd: false,
		},
		{
			Name: "ecosystem_fund", Address: a.EcosystemFund,
			TotalSCRT: 178_000_000, LiquidPct: 100,
			Kind: VestNone,
		},
		{
			Name: "research_development", Address: a.ResearchDevelopment,
			TotalSCRT: 72_000_000, LiquidPct: 10,
			Kind: VestContinuous, CliffMonths: 0, DurationMonths: 60,
		},
		{
			// v24 moved this to the unshaded rows: "Existing Holders, Ecosystem
			// Fund, Builder & Relayer Support: no vesting or lockup."
			// (v23 had it at 10% liquid with 90% vesting linearly over 5y.)
			Name: "builder_relayer_support", Address: a.BuilderRelayerSupport,
			TotalSCRT: 43_000_000, LiquidPct: 100,
			Kind: VestNone,
		},
		{
			Name: "remediation", Address: a.Remediation,
			TotalSCRT: 44_000_000, LiquidPct: 30,
			Kind: VestContinuous, CliffMonths: 6, DurationMonths: 24,
		},
	}
}

// ValidatorProgram returns this network's validator program configuration.
func (n Network) ValidatorProgram() ValidatorProgram {
	return ValidatorProgram{
		ProgramAddress:     n.Addresses.ValidatorProgram,
		TotalSCRT:          72_000_000,
		UpfrontCapPct:      10,
		VestDurationMonths: 60,
	}
}

// openSeats returns a registry of ValidatorSeatCount empty seats.
func openSeats() []ValidatorSeat { return make([]ValidatorSeat, ValidatorSeatCount) }

// Mainnet is the secret-4 build.
//
// Addresses stay FillMe until testnet is user-confirmed GREEN — that gate is
// deliberate, see plan-staging/mainnet/CHECKLIST.md.
//
// EVERY address must be seeded with 1 uscrt at key-generation time, BEFORE it
// is written here or shared with anyone (CHECKLIST M1s).
var Mainnet = Network{
	Name:    "mainnet",
	ChainID: "secret-4",
	Addresses: AddressSet{
		Foundation:            FillMe,
		CoreDevelopment:       FillMe,
		Advisors:              FillMe,
		EcosystemFund:         FillMe,
		ResearchDevelopment:   FillMe,
		BuilderRelayerSupport: FillMe,
		Remediation:           FillMe,
		ValidatorProgram:      FillMe,
	},
	Seats: openSeats(),
}

// Pulsar is the testnet build. Its upgrade name differs from mainnet's on
// purpose: a binary built for one network cannot answer the other's plan.
//
// Fill these with THROWAWAY keys — seeded with 1 uscrt at generation time,
// before being written here (CHECKLIST T6a).
var Pulsar = Network{
	Name:    "testnet",
	ChainID: "pulsar-3",
	Addresses: AddressSet{
		Foundation:            "secret1mcevwvr8n3nkhv35dmwkm7zfqpqyypdn7hcryk",
		CoreDevelopment:       "secret1p3l02sqqddt8n4kg9vs8qeq0hvmx4g6mpdc8w9",
		Advisors:              "secret1d3n3cjkpwemav70xt5wtpmpt0nuy0jl2md6lvz",
		EcosystemFund:         "secret167ewv0c5cg5k8xwjtmsr8stsxfgxpyc32qed8h",
		ResearchDevelopment:   "secret1dpplrnf75kw7sfyv72msq9kua0qycpq55wrmxe",
		BuilderRelayerSupport: "secret1j8w9qym77v787lec09c7gxn7pvvugrwkjyjk6n",
		Remediation:           "secret1q8ksklhfjc9rsduy6cw2nl2xfjsuczht4lzjp9",
		ValidatorProgram:      "secret1kt67upwgqhjgeh6smh4ey9mhgzrtv4wle8jr7u",
	},
	Seats: func() []ValidatorSeat {
		s := openSeats()
		s[0] = ValidatorSeat{Operator: "secretvaloper17nl6x709q8wta9ja0qu0kduvfc70u8fskhkkve", Address: "secret1qrn9yzzhea8g94r839q2dv8aq9wy6s9g99ayr4"}  // SNF-B
		s[1] = ValidatorSeat{Operator: "secretvaloper1fj9zf2l6xlashn3sqcvuj7lythahg3f2v9mx2z", Address: "secret1sh5zgy007t62upyd4vgegq69qeffqu5uznvyuz"}  // SNF-D
		s[2] = ValidatorSeat{Operator: "secretvaloper1lgre6vtvntv75z6zvghkpwy4m953u4a7dc8e6e", Address: "secret1770dqwlmmnt74588pvj8uqevhx9hjaalngyxmu"}  // SNF-A
		s[3] = ValidatorSeat{Operator: "secretvaloper1ncfe2lwe0hm4fcjj3ma8nw9xxy95e7dvl4ppyj", Address: "secret19x0u83dfynl0sm8evt6l85cg668fttyf7frw5h"}  // SNF-E
		s[4] = ValidatorSeat{Operator: "secretvaloper14lk5dz0rwk2l47dktvhdvfkxrpmeya2q4s94c5", Address: "secret1xrn0rk3dayv3u65crlqtyky62kecr4txw7sjw3"}  // ghost-05
		s[5] = ValidatorSeat{Operator: "secretvaloper14zgml4pfh03fwlrzrplwttlt32kk3qeyuwnkyu", Address: "secret1fe0sjvgcktdjgeyup4xy4lnrceyhcr44hd5unx"}  // ghost-06
		s[6] = ValidatorSeat{Operator: "secretvaloper13yv6rmhx65cetg7d4xr7mt3xccfk3kcrfky09s", Address: "secret1dl77cj4x3urxsw0y4qpetw3dwl50mlyk6vd5r6"}  // ghost-07
		s[7] = ValidatorSeat{Operator: "secretvaloper1xd5fseql7ztz9ua9vkrwwxjsevgdeecvfuakqm", Address: "secret178289rxvsyjqqm5832n7kmzyn4kaqzskf2r5eg"}  // ghost-08
		s[8] = ValidatorSeat{Operator: "secretvaloper16vgd2tnxat6w93dqps56d2psu6ykr8fa553ue8", Address: "secret10aspggnyhu8hfq99n37nja6u27yarqrkte20vp"}  // ghost-09
		s[9] = ValidatorSeat{Operator: "secretvaloper1x0uugkqpsun8f9x4dhxyvyulmfzyzh8gxhfrv4", Address: "secret1yqtvp62rm8suxq7u92y7u4xnt7yehs3c5zr5lc"}  // ghost-10
		s[10] = ValidatorSeat{Operator: "secretvaloper1kul5rvx9x7q4zpf3gxx8t0x9fg5d2ea36nt7zp", Address: "secret1dt9t0tqqdpm47fjtjn34phf58vg3ew6tgugehu"}  // ghost-11
		s[11] = ValidatorSeat{Operator: "secretvaloper1fyaerpht0qxjkeflhn3ue9su2vzs3t9tr68zsk", Address: "secret1syg4cdyq9xtrkyvepumqn6ruqghm4w72vrnqxe"}  // ghost-12
		s[12] = ValidatorSeat{Operator: "secretvaloper16gwg30ez4yuau8nj54fzxser6vtapqt0zw870u", Address: "secret1h065msw39lam8zzpcpu4hufkqgaqkn93rxvusx"}  // ghost-13
		s[13] = ValidatorSeat{Operator: "secretvaloper1ue7chn086qr2zp2dfyxw5xrcfyh6fll42h0ghq", Address: "secret185d23970ttwr2r98a46cy0vx0numteg44gkgax"}  // ghost-14
		s[14] = ValidatorSeat{Operator: "secretvaloper1evt8jwp3n862eme8svssp5r9549de9teu5uke5", Address: "secret16ys07rfcf4ntymz7wjams6phlu3a9kpa88n0yk"}  // ghost-15
		s[15] = ValidatorSeat{Operator: "secretvaloper10u8uug4p2e9hqsdg3dezlq2x3vg8da9u28g9am", Address: "secret1mx7p80e083n68gvj57ruaewnwah5xcx87qgxae"}  // ghost-16
		s[16] = ValidatorSeat{Operator: "secretvaloper12ggcteulc3jcm0mrat9php4cr2cn2t2zv43dqw", Address: "secret12namkf2mw05r0u9x27498gfwm59h99h964wkxd"}  // ghost-17
		s[17] = ValidatorSeat{Operator: "secretvaloper190lq8cvw78tasmshgfygzha4s7gneg65w34klz", Address: "secret15dwx5gfgk9lfpym7udkprnsfr9cuh9t2nwawcp"}  // ghost-18
		s[18] = ValidatorSeat{Operator: "secretvaloper17tsmp92qa0vnmmguxgyt8ts56fxlucu8qgvgez", Address: "secret17tjs40v0klummefu8v3e4esdvzfz2nvmy3cx3w"}  // ghost-19
		s[19] = ValidatorSeat{Operator: "secretvaloper1m5un9r6nnd8tfmanx8yzkc8vwpkqrut7uyyc3f", Address: "secret1h0ek4drx0ekr4ymjrnmnphrfc7fhwm2ksts8xw"}  // ghost-20
		s[20] = ValidatorSeat{Operator: "secretvaloper1k89wdls25qgh26fdtvgamx50cy8lynxdqtc3yx", Address: "secret1scp795p8k68x97het0zd7jhffzcq0cec73ga8j"}  // ghost-21
		s[21] = ValidatorSeat{Operator: "secretvaloper10x8rev7ee749za6vjsrv25akuxajuvx9gc82c2", Address: "secret1kh27zj3z9jp8ln2w3w325xz47c89erkl929kll"}  // ghost-22
		s[22] = ValidatorSeat{Operator: "secretvaloper18wrctgyeetpgsqcq63qdjuqrle3v08fnpfz0fx", Address: "secret1aw7cenqpct3f4hdtty9r44csjqrgrxzk05w7lk"}  // ghost-23
		s[23] = ValidatorSeat{Operator: "secretvaloper1v20xdnz25n9r0lzzndchxn6327lggd86y9zlhu", Address: "secret1g05kzj4lsykq2xapfdng8h300d82at3qhgjz5t"}  // ghost-24
		s[24] = ValidatorSeat{Operator: "secretvaloper10zxu8alkrv64vsyvudpmcg8jpg5e8pn09wx2qu", Address: "secret1rekcztmuzms7cg9haew8tkpp3tz963pv0q9ez8"}  // ghost-25
		s[25] = ValidatorSeat{Operator: "secretvaloper1p9g9p7hvhg43u3x2mmmerwx6mqnvemkx2jtz8l", Address: "secret12q440gn26ahf93wp9rcnluh2ppdsf8cy4fqv0s"}  // ghost-26
		s[26] = ValidatorSeat{Operator: "secretvaloper1z4extxzucn7vv05g8jmntagv4wxklm2vjpys2e", Address: "secret1t8rwfysdwuervjk48p0lze8a54gyyktlynllve"}  // ghost-27
		s[27] = ValidatorSeat{Operator: "secretvaloper1gdp2t79q06nwl0r3xx289kwc8yc0rr5jl0zvv7", Address: "secret1avkyh7zh3a9q08k73wczd6cqp3suevxtj7q6gs"}  // ghost-28
		s[28] = ValidatorSeat{Operator: "secretvaloper1uyy0mnn8q7thdwftg3mu42nyz7qzsdxtputtm4", Address: "secret1dhz7re83zpmdy6j3f0mk9h2cmgers8x8mhjvnv"}  // ghost-29
		s[29] = ValidatorSeat{Operator: "secretvaloper14573upqndeqrqk3q8kp3la709qtzsah88xfzv0", Address: "secret1zjntucf7w36qqhgktl9njsghvrvlnhe8deayqu"}  // ghost-30
		return s
	}(),
}

// LocalSecret is a Network for chain ID secretdev-1 so local rehearsal can
// exercise the same handler path. Without this entry, ForChainID refuses
// secretdev-1 by design.
//
// Carrying it in every binary is harmless: ForChainID only ever returns it on a
// chain calling itself "secretdev-1". The upgrade NAME is the same constant on
// every chain — there is no "-local" variant — so a LocalSecret gov proposal
// must use "v1.26.0-community-continuance" like everywhere else.
var LocalSecret = Network{
	Name:    "localsecret",
	ChainID: "secretdev-1",
	Addresses: AddressSet{
		Foundation:            "secret1wffsut90afflsyczke4t0gp2gnwq74vq5jq58x",
		CoreDevelopment:       "secret1kszu96u4jq2qd2095h06pmlydjz7xyvpqqf3jy",
		Advisors:              "secret14pjeq3je6963w92aeafewdh5cclz44n5a3tvrf",
		EcosystemFund:         "secret1fl9ymfxhl3gxkv5z235lyzpnlgqa7an0wmt42y",
		ResearchDevelopment:   "secret10sraz9knyqhaew6p73y5aeq38u3hgze6zdx025",
		BuilderRelayerSupport: "secret1ks6xfmwkqh423xtf9vsnn9ums4tfmrfnuvflmq",
		Remediation:           "secret1dxsc20sx7ap9dncz87gnz4xxnv6khdpwpktp4m",
		ValidatorProgram:      "secret1ganaexupppt5uwsgfz6yjrhmge5kuk29ujcgy7",
	},
	Seats: func() []ValidatorSeat {
		s := openSeats()
		s[0] = ValidatorSeat{Operator: "secretvaloper14aj08vd2ntty7dvskdmdu4zhf23mcwgtdvh6qt", Address: "secret1fhu00xztmhje8qe44jczc0z5edv772xpmmvldc"}
		s[1] = ValidatorSeat{Operator: "secretvaloper13mvtw25rhumgnrqd8qpzte0p8naj88s2p5w30p", Address: "secret167hk7sy26xnw02lduuz6ru4s2cnutmpx6xyjly"}
		return s
	}(),
}

// Networks is every chain this upgrade knows how to run on.
var Networks = []Network{Mainnet, Pulsar, LocalSecret}

// ForChainID selects the network to use, from the chain the handler is actually
// running on. This replaces a build-time `Active` variable on purpose.
//
// With a compile-time switch, the binary's ON-CHAIN IDENTITY depended on a
// one-line source edit: cut a mainnet binary without flipping it and secret-4's
// governance plan has no registered handler, which halts every node at the
// upgrade height. Nothing in a normal `go test ./...` could catch that.
//
// Selecting at runtime removes the failure mode entirely — there is no such
// thing as a "wrong-network build" — matching prior Secret upgrade handlers.
//
// An unknown chain is a hard error: the handler refuses to mint rather than
// guessing which custody addresses apply.
func ForChainID(chainID string) (Network, error) {
	for _, n := range Networks {
		if n.ChainID == chainID {
			return n, nil
		}
	}
	known := make([]string, 0, len(Networks))
	for _, n := range Networks {
		known = append(known, n.ChainID)
	}
	return Network{}, fmt.Errorf("no Continuance configuration for chain-id %q (known: %v) — refusing to mint", chainID, known)
}

// ---------------------------------------------------------------------------
// Validation — the release gate, and the handler's first act at the height
// ---------------------------------------------------------------------------

// Validate checks everything that can be checked without a running chain.
// The upgrade handler calls this before minting; config_test.go calls it at
// build time. A failure here at build time is the intended outcome while
// placeholders remain.
func Validate(n Network) error {
	if n.ChainID == "" {
		return fmt.Errorf("network %q: ChainID must be set", n.Name)
	}

	// Every address across buckets AND seats must be distinct — a repeat
	// would silently double-fund one address and starve another.
	//
	// Dedup on the CANONICAL form (decode, then re-encode lower-case), never
	// on the raw input string. bech32 is case-insensitive, so "secret1abc…"
	// and "SECRET1ABC…" are the same account but different strings. Keying on
	// the raw string would let a case-variant duplicate pass this gate and
	// then halt the chain at the upgrade height — after the mint — when the
	// second bucket found the first one's vesting account already there.
	seen := map[string]string{}
	seenOperators := map[string]string{}
	checkAddr := func(what, bech string) error {
		addr, err := sdk.AccAddressFromBech32(bech)
		if err != nil {
			return fmt.Errorf("%s: %q is not a valid bech32 account address (placeholder not replaced?): %w", what, bech, err)
		}
		// SDK default verifier allows short payloads that decode as bech32.
		// Real secp256k1 account addresses are exactly 20 bytes.
		if len(addr) != sdkAddrLen {
			return fmt.Errorf("%s: %q decodes to %d bytes, not %d — this is not a normal account address", what, bech, len(addr), sdkAddrLen)
		}
		// A module account here is never intentional, and one of them fails
		// silently rather than loudly — see moduleAccountNames.
		if name, isMod := isModuleAddress(addr); isMod {
			return fmt.Errorf("%s: address %s is the %q MODULE ACCOUNT — funds sent there are unrecoverable; this is never a valid custody or payout address", what, bech, name)
		}
		key := addr.String() // canonical, lower-case bech32
		if prev, dup := seen[key]; dup {
			return fmt.Errorf("%s: address %s is already used by %s — every address must be distinct", what, bech, prev)
		}
		seen[key] = what
		return nil
	}

	allocs := n.Allocations()
	var sumSCRT int64

	for _, a := range allocs {
		if err := checkAddr(a.Name, a.Address); err != nil {
			return err
		}
		if a.TotalSCRT <= 0 {
			return fmt.Errorf("%s: TotalSCRT must be positive", a.Name)
		}
		if a.LiquidPct < 0 || a.LiquidPct > 100 {
			return fmt.Errorf("%s: LiquidPct must be within [0,100]", a.Name)
		}
		if a.CliffMonths < 0 {
			return fmt.Errorf("%s: CliffMonths cannot be negative", a.Name)
		}
		switch a.Kind {
		case VestNone:
			if a.LiquidPct != 100 {
				return fmt.Errorf("%s: VestNone requires LiquidPct == 100", a.Name)
			}
		case VestContinuous:
			if a.LiquidPct == 100 {
				return fmt.Errorf("%s: VestContinuous with LiquidPct 100 locks nothing — use VestNone", a.Name)
			}
			if a.DurationMonths <= 0 {
				return fmt.Errorf("%s: DurationMonths must be positive", a.Name)
			}
		case VestPeriodicQuarterly:
			if a.LiquidPct == 100 {
				return fmt.Errorf("%s: VestPeriodicQuarterly with LiquidPct 100 locks nothing — use VestNone", a.Name)
			}
			if a.DurationMonths <= 0 || a.DurationMonths%3 != 0 {
				return fmt.Errorf("%s: DurationMonths must be a positive multiple of 3", a.Name)
			}
		default:
			return fmt.Errorf("%s: unknown vest kind %d", a.Name, a.Kind)
		}
		sumSCRT += a.TotalSCRT
	}

	vp := n.ValidatorProgram()
	if err := checkAddr("validator_program", vp.ProgramAddress); err != nil {
		return err
	}
	if vp.TotalSCRT <= 0 {
		return fmt.Errorf("validator_program: TotalSCRT must be positive")
	}
	if vp.UpfrontCapPct < 0 || vp.UpfrontCapPct > 100 {
		return fmt.Errorf("validator_program: UpfrontCapPct must be within [0,100]")
	}
	if vp.VestDurationMonths <= 0 || vp.VestDurationMonths%3 != 0 {
		return fmt.Errorf("validator_program: VestDurationMonths must be a positive multiple of 3")
	}
	sumSCRT += vp.TotalSCRT

	// The announced mint total is enforced here, so a mistyped bucket size
	// cannot reach a binary. This is the check the old tautological
	// "minted == distributed" invariant failed to provide.
	if sumSCRT != TotalMintSCRT {
		return fmt.Errorf("allocation table sums to %d SCRT but the proposal commits to %d SCRT — refusing to mint a different supply",
			sumSCRT, TotalMintSCRT)
	}

	// Seat pool must divide exactly; no dust, no rounding discretion.
	pool := vp.UpfrontPoolSCRT()
	if pool%int64(ValidatorSeatCount) != 0 {
		return fmt.Errorf("validator_program: upfront pool %d SCRT does not divide evenly across %d seats", pool, ValidatorSeatCount)
	}
	if perSeat := pool / int64(ValidatorSeatCount); perSeat != ReservedSeatSCRT {
		return fmt.Errorf("validator_program: ReservedSeatSCRT is %d but pool/seats is %d", ReservedSeatSCRT, perSeat)
	}
	if len(n.Seats) != ValidatorSeatCount {
		return fmt.Errorf("validator_program: expected exactly %d seats, got %d", ValidatorSeatCount, len(n.Seats))
	}

	for i, s := range n.Seats {
		slot := i + 1
		if s.Operator == "" && s.Address == "" {
			continue // vacant seat — its 240,000 SCRT waits at the program address
		}
		if s.Partial() {
			return fmt.Errorf("validator slot %02d is half-filled — Operator and Address are required together", slot)
		}
		name := fmt.Sprintf("validator slot %02d (%s)", slot, s.Operator)
		if err := checkAddr(name, s.Address); err != nil {
			return err
		}
		valAddr, err := sdk.ValAddressFromBech32(s.Operator)
		if err != nil {
			return fmt.Errorf("%s: Operator is not a valid secretvaloper address: %w", name, err)
		}
		// Same length hole as account addresses: the SDK's default verifier
		// accepts any 1–255 byte payload, so a stub like "secretvaloper1q5wm8dsp"
		// decodes cleanly. A malformed operator would not misdirect money — the
		// seat is skipped at the height — but it would silently deny a real
		// validator their 240,000 SCRT, which is exactly what M1b exists to catch.
		if len(valAddr) != sdkAddrLen {
			return fmt.Errorf("%s: Operator %q decodes to %d bytes, not %d — this is not a normal validator address", name, s.Operator, len(valAddr), sdkAddrLen)
		}
		// One seat per validator. Two seats naming the same operator would pay
		// that validator twice — 480,000 SCRT across two payout addresses —
		// while starving a seat that should have gone to someone else.
		// Canonicalized for the same case-insensitivity reason as addresses.
		opKey := valAddr.String()
		if prev, dup := seenOperators[opKey]; dup {
			return fmt.Errorf("%s: validator %s already holds %s — one seat per validator", name, s.Operator, prev)
		}
		seenOperators[opKey] = name
	}

	// Dry-run every schedule the handler will build, at block times that
	// stress calendar arithmetic. A schedule that cannot be constructed must
	// fail the build, never the upgrade height.
	for _, at := range scheduleProbeTimes() {
		if err := dryRunSchedules(n, at); err != nil {
			return fmt.Errorf("schedule dry-run failed for block time %s: %w", at.Format(time.RFC3339), err)
		}
	}

	return nil
}

// scheduleProbeTimes returns block times chosen to stress calendar-month
// arithmetic: a normal mid-month day, a 28th, month ends, and a leap day.
func scheduleProbeTimes() []time.Time {
	return []time.Time{
		time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 28, 23, 59, 59, 0, time.UTC),
		time.Date(2026, 8, 29, 0, 0, 1, 0, time.UTC),
		time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 12, 31, 23, 0, 0, 0, time.UTC),
		time.Date(2028, 2, 29, 6, 0, 0, 0, time.UTC), // leap day
	}
}

// dryRunSchedules constructs every vesting schedule for the given block time,
// exactly as the handler would, and validates the resulting accounts' inputs.
func dryRunSchedules(n Network, at time.Time) error {
	for _, a := range n.Allocations() {
		switch a.Kind {
		case VestNone:
			// nothing to construct
		case VestContinuous:
			start, end := ContinuousWindow(at, a)
			if !end.After(start) {
				return fmt.Errorf("bucket %s: vesting end %s is not after start %s", a.Name, end, start)
			}
		case VestPeriodicQuarterly:
			periods, err := BuildQuarterlyPeriods(at, a.CliffMonths, a.DurationMonths, a.FirstUnlockAtCliffEnd, a.Locked())
			if err != nil {
				return fmt.Errorf("bucket %s: %w", a.Name, err)
			}
			if err := checkPeriods(periods, a.Locked()); err != nil {
				return fmt.Errorf("bucket %s: %w", a.Name, err)
			}
		}
	}

	vp := n.ValidatorProgram()
	locked := ToUscrt(vp.VestingSCRT())
	periods, err := BuildQuarterlyPeriods(at, 0, vp.VestDurationMonths, false, locked)
	if err != nil {
		return fmt.Errorf("validator_program: %w", err)
	}
	if err := checkPeriods(periods, locked); err != nil {
		return fmt.Errorf("validator_program: %w", err)
	}
	return nil
}

// checkPeriods enforces what NewPeriodicVestingAccount.Validate() enforces:
// positive lengths and amounts summing exactly to the locked total.
func checkPeriods(periods vestingtypes.Periods, locked sdkmath.Int) error {
	if len(periods) == 0 {
		return fmt.Errorf("no vesting periods produced")
	}
	sum := sdkmath.ZeroInt()
	for i, p := range periods {
		if p.Length <= 0 {
			return fmt.Errorf("period %d has non-positive length %d", i, p.Length)
		}
		sum = sum.Add(p.Amount.AmountOf(BondDenom))
	}
	if !sum.Equal(locked) {
		return fmt.Errorf("periods sum to %s but locked total is %s", sum, locked)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Schedule construction — shared by the handler and the release gate
// ---------------------------------------------------------------------------

// ContinuousWindow returns the (start, end) of a continuous bucket's vesting,
// anchored at the upgrade block time using calendar months.
//
// NOTE ON CALENDAR ARITHMETIC: Go normalizes overflowing days, so a block
// landing on the 29th–31st shifts the whole schedule by a day or three
// (e.g. Aug 31 + 6 months → Mar 3). The handler warns when this applies.
// Published unlock dates must be derived from the actual block time, not
// assumed to fall on the same day of the month.
func ContinuousWindow(at time.Time, a Allocation) (start, end time.Time) {
	start = at.AddDate(0, a.CliffMonths, 0)
	end = start.AddDate(0, a.DurationMonths, 0)
	return start, end
}

// BuildQuarterlyPeriods produces equal-AMOUNT quarterly unlocks of `locked`
// over durationMonths, delayed by cliffMonths, anchored at `start` (which is
// the vesting account's StartTime).
//
// The periods are equal in amount but NOT in length: period 0 spans from the
// upgrade block to the first unlock, so it absorbs the cliff. Later periods
// run 89–92 days depending on which calendar months they cross. Any published
// schedule must say this rather than claiming "20 equal quarters".
//
// Integer dust from the equal split is folded into the final period so the
// amounts sum exactly to `locked`, which NewPeriodicVestingAccount requires.
func BuildQuarterlyPeriods(start time.Time, cliffMonths, durationMonths int, firstAtCliffEnd bool, locked sdkmath.Int) (vestingtypes.Periods, error) {
	if durationMonths <= 0 || durationMonths%3 != 0 {
		return nil, fmt.Errorf("durationMonths must be a positive multiple of 3, got %d", durationMonths)
	}
	if cliffMonths < 0 {
		return nil, fmt.Errorf("cliffMonths cannot be negative, got %d", cliffMonths)
	}
	if locked.IsNegative() {
		return nil, fmt.Errorf("locked amount cannot be negative")
	}

	n := durationMonths / 3
	cliffEnd := start.AddDate(0, cliffMonths, 0)
	per := locked.QuoRaw(int64(n))

	remaining := locked
	periods := make(vestingtypes.Periods, 0, n)
	prev := start

	for i := 0; i < n; i++ {
		var unlockAt time.Time
		if firstAtCliffEnd {
			unlockAt = cliffEnd.AddDate(0, 3*i, 0)
		} else {
			unlockAt = cliffEnd.AddDate(0, 3*(i+1), 0)
		}

		length := unlockAt.Unix() - prev.Unix()
		if length <= 0 {
			return nil, fmt.Errorf("non-positive period length at unlock %d (cliff %d months, firstAtCliffEnd=%v)", i, cliffMonths, firstAtCliffEnd)
		}

		amt := per
		if i == n-1 {
			amt = remaining // fold rounding dust into the last unlock
		}
		remaining = remaining.Sub(amt)

		periods = append(periods, vestingtypes.Period{
			Length: length,
			Amount: sdk.NewCoins(sdk.NewCoin(BondDenom, amt)),
		})
		prev = unlockAt
	}

	if !remaining.IsZero() {
		return nil, fmt.Errorf("internal error: %s left unassigned after building periods", remaining)
	}
	return periods, nil
}

// ToUscrt converts whole SCRT to uscrt.
func ToUscrt(scrt int64) sdkmath.Int {
	return sdkmath.NewInt(scrt).MulRaw(MicroUnitsPerSCRT)
}
