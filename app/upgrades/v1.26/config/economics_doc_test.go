package config

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestEconomicsDocIsCurrent keeps the published economics table honest by
// GENERATING it from this package rather than trusting anyone to update prose.
//
// Docs go stale because a number lives in two places. This makes the code the
// only place: change an allocation and this test fails until the doc is
// regenerated, and CI runs it on every push.
//
//	regenerate:  REGENERATE_ECONOMICS=1 go test ./app/upgrades/v1.26/config/ -run EconomicsDoc
func TestEconomicsDocIsCurrent(t *testing.T) {
	const path = "ECONOMICS.md"
	got := renderEconomics()

	if os.Getenv("REGENERATE_ECONOMICS") != "" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("regenerated %s", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s missing — run: REGENERATE_ECONOMICS=1 go test ./app/upgrades/v1.26/config/ -run EconomicsDoc", path)
	}
	if string(want) != got {
		t.Fatalf("%s is STALE — the code changed and the doc did not.\n"+
			"Regenerate: REGENERATE_ECONOMICS=1 go test ./app/upgrades/v1.26/config/ -run EconomicsDoc", path)
	}
}

// renderEconomics derives every published figure from the live config.
func renderEconomics() string {
	n := Network{Addresses: AddressSet{}} // shapes only; addresses irrelevant
	var b strings.Builder

	b.WriteString("# Economics — GENERATED, do not edit\n\n")
	b.WriteString("Generated from `app/upgrades/v1.26/config/config.go` by\n")
	b.WriteString("`TestEconomicsDocIsCurrent`. Editing this file by hand will be overwritten,\n")
	b.WriteString("and CI fails if the code changes without regenerating.\n\n")
	b.WriteString("**Every other document links here instead of restating these numbers.**\n\n")

	b.WriteString("| Bucket | Total SCRT | Liquid at execution | Lockup |\n")
	b.WriteString("|---|---:|---:|---|\n")

	var totalSCRT, liquidUscrt int64
	for _, a := range n.Allocations() {
		var shape string
		switch a.Kind {
		case VestNone:
			shape = "none — fully liquid"
		case VestContinuous:
			if a.CliffMonths > 0 {
				shape = fmt.Sprintf("linear over %d months, after a %d-month cliff", a.DurationMonths, a.CliffMonths)
			} else {
				shape = fmt.Sprintf("linear over %d months, no cliff", a.DurationMonths)
			}
		case VestPeriodicQuarterly:
			q := a.DurationMonths / 3
			per := a.Locked().QuoRaw(int64(q)).QuoRaw(MicroUnitsPerSCRT)
			shape = fmt.Sprintf("%d-month cliff, then %d quarterly unlocks of %s SCRT", a.CliffMonths, q, commas(per.Int64()))
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %d%% | %s |\n",
			a.Name, commas(a.TotalSCRT), a.LiquidPct, shape))
		totalSCRT += a.TotalSCRT
		liquidUscrt += a.Liquid().Int64()
	}

	vp := n.ValidatorProgram()
	q := vp.VestDurationMonths / 3
	b.WriteString(fmt.Sprintf("| validator_program | %s | %d%% seat pool | %s SCRT quarterly-vested at the program address over %d months (%d unlocks) |\n",
		commas(vp.TotalSCRT), vp.UpfrontCapPct, commas(vp.VestingSCRT()), vp.VestDurationMonths, q))
	totalSCRT += vp.TotalSCRT
	liquidUscrt += ToUscrt(vp.UpfrontPoolSCRT()).Int64()

	liquidSCRT := liquidUscrt / MicroUnitsPerSCRT
	b.WriteString(fmt.Sprintf("| **Total** | **%s** | **%s** | |\n\n", commas(totalSCRT), commas(liquidSCRT)))

	b.WriteString("## Derived figures\n\n")
	b.WriteString(fmt.Sprintf("- Mint total: **%s SCRT** (asserted equal to `TotalMintSCRT`)\n", commas(totalSCRT)))
	b.WriteString(fmt.Sprintf("- Day-one liquid: **%s SCRT**\n", commas(liquidSCRT)))
	b.WriteString(fmt.Sprintf("- Validator seats: **%d × %s SCRT** = %s SCRT seat pool\n",
		ValidatorSeatCount, commas(ReservedSeatSCRT), commas(vp.UpfrontPoolSCRT())))
	b.WriteString(fmt.Sprintf("- Inflation pinned at: **%s**\n", TargetInflation))
	b.WriteString(fmt.Sprintf("- Secret foundation tax zeroed: **%v**\n", ZeroFoundationTax))
	b.WriteString(fmt.Sprintf("- Upgrade name (all chains): **`%s`**\n", UpgradeName))
	b.WriteString("- Chains recognised: ")
	for i, net := range Networks {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(fmt.Sprintf("`%s` (%s)", net.ChainID, net.Name))
	}
	b.WriteString("\n")
	return b.String()
}

func commas(v int64) string {
	s := fmt.Sprintf("%d", v)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
