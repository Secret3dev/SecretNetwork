package types

import (
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Privileged message identifiers. Match proto TypeURL, amino name, and Type().
const (
	MsgUpgradeProposalPassedTypeURL          = "/secret.compute.v1beta1.MsgUpgradeProposalPassed"
	MsgUpdateMachineWhitelistTypeURL         = "/secret.compute.v1beta1.MsgUpdateMachineWhitelist"
	MsgUpgradeProposalPassedAminoName        = "wasm/MsgUpgradeProposalPassed"
	MsgUpdateMachineWhitelistAminoName       = "wasm/MsgUpdateMachineWhitelist"
	MsgUpgradeProposalPassedTypeName         = "upgrade-proposal-passed"
	MsgUpdateMachineWhitelistTypeName        = "update-machine-whitelist"
	MsgSoftwareUpgradeTypeURL                = "/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade"
	MsgUpdateMachineWhitelistProposalTypeURL = "/secret.compute.v1beta1.MsgUpdateMachineWhitelistProposal"
)

// Layout of the measurement-update value checked by the enclave.
const (
	MrenclaveProtoLen     = 81
	MrenclaveProtoPrefix0 = 0x0a
	MrenclaveProtoPrefix1 = 0x2d
	MrenclaveProtoMid0    = 0x12
	MrenclaveProtoMid1    = 0x20
	MrenclaveProtoMidOff  = 47
)

func normalizeTypeURL(s string) string {
	return strings.TrimSpace(s)
}

func matchAny(s string, names ...string) bool {
	s = normalizeTypeURL(s)
	if s == "" {
		return false
	}
	for _, n := range names {
		if s == n || s == strings.TrimPrefix(n, "/") {
			return true
		}
	}
	return false
}

func IsUpgradeProposalPassedTypeURL(s string) bool {
	return matchAny(s,
		MsgUpgradeProposalPassedTypeURL,
		MsgUpgradeProposalPassedAminoName,
		MsgUpgradeProposalPassedTypeName,
	)
}

func IsUpdateMachineWhitelistTypeURL(s string) bool {
	return matchAny(s,
		MsgUpdateMachineWhitelistTypeURL,
		MsgUpdateMachineWhitelistAminoName,
		MsgUpdateMachineWhitelistTypeName,
	)
}

func IsPrivilegedTypeURL(s string) bool {
	return IsUpgradeProposalPassedTypeURL(s) || IsUpdateMachineWhitelistTypeURL(s)
}

func IsSoftwareUpgradeTypeURL(s string) bool {
	return matchAny(s, MsgSoftwareUpgradeTypeURL)
}

type hasLegacyType interface {
	Type() string
}

func legacyType(msg sdk.Msg) string {
	if m, ok := msg.(hasLegacyType); ok {
		return m.Type()
	}
	return ""
}

func IsPrivilegedMsg(msg sdk.Msg) bool {
	if msg == nil {
		return false
	}
	if IsPrivilegedTypeURL(sdk.MsgTypeURL(msg)) {
		return true
	}
	return IsPrivilegedTypeURL(legacyType(msg))
}

func IsUpgradeProposalPassedMsg(msg sdk.Msg) bool {
	if msg == nil {
		return false
	}
	if _, ok := msg.(*MsgUpgradeProposalPassed); ok {
		return true
	}
	if IsUpgradeProposalPassedTypeURL(sdk.MsgTypeURL(msg)) {
		return true
	}
	return IsUpgradeProposalPassedTypeURL(legacyType(msg))
}

func IsUpdateMachineWhitelistMsg(msg sdk.Msg) bool {
	if msg == nil {
		return false
	}
	if _, ok := msg.(*MsgUpdateMachineWhitelist); ok {
		return true
	}
	if IsUpdateMachineWhitelistTypeURL(sdk.MsgTypeURL(msg)) {
		return true
	}
	return IsUpdateMachineWhitelistTypeURL(legacyType(msg))
}

// LooksLikeMrenclaveProtoValue reports whether the bytes use the measurement-update layout.
func LooksLikeMrenclaveProtoValue(b []byte) bool {
	return len(b) == MrenclaveProtoLen &&
		b[0] == MrenclaveProtoPrefix0 &&
		b[1] == MrenclaveProtoPrefix1 &&
		b[MrenclaveProtoMidOff] == MrenclaveProtoMid0 &&
		b[MrenclaveProtoMidOff+1] == MrenclaveProtoMid1
}
