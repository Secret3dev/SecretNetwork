package app

import (
	"testing"

	v1_26 "github.com/scrtlabs/SecretNetwork/app/upgrades/v1.26"
	v1_26p "github.com/scrtlabs/SecretNetwork/app/upgrades/v1.26p"
	v1_27 "github.com/scrtlabs/SecretNetwork/app/upgrades/v1.27"
	v1_27_1 "github.com/scrtlabs/SecretNetwork/app/upgrades/v1.27.1"
	v1_27_2 "github.com/scrtlabs/SecretNetwork/app/upgrades/v1.27.2"
	"github.com/stretchr/testify/require"
)

func TestUpgradesKeepV126AndRegisterV127(t *testing.T) {
	names := map[string]bool{}
	order := make([]string, 0, len(Upgrades))
	for _, u := range Upgrades {
		names[u.UpgradeName] = true
		order = append(order, u.UpgradeName)
	}
	require.True(t, names[v1_26.UpgradeName], "v1_26 Continuance handler must stay registered")
	require.True(t, names[v1_26p.Upgrade.UpgradeName], "v1_26p must stay registered")
	require.True(t, names[v1_27.UpgradeName], "v1_27 must be registered")
	require.True(t, names[v1_27_1.UpgradeName], "v1_27_1 must be registered")
	require.True(t, names[v1_27_2.UpgradeName], "v1_27_2 must be registered")
	require.False(t, names["v1.23.3"], "do not register v1.23.3")
	require.Equal(t, "v1.27.0", v1_27.UpgradeName)
	require.Equal(t, "v1.27.1", v1_27_1.UpgradeName)
	require.Equal(t, "v1.27.2", v1_27_2.UpgradeName)
	require.Empty(t, v1_27.Upgrade.StoreUpgrades.Added)
	require.Empty(t, v1_27.Upgrade.StoreUpgrades.Renamed)
	require.Empty(t, v1_27.Upgrade.StoreUpgrades.Deleted)

	i26, i26p, i27, i271, i272 := -1, -1, -1, -1, -1
	for i, n := range order {
		switch n {
		case v1_26.UpgradeName:
			i26 = i
		case v1_26p.Upgrade.UpgradeName:
			i26p = i
		case v1_27.UpgradeName:
			i27 = i
		case v1_27_1.UpgradeName:
			i271 = i
		case v1_27_2.UpgradeName:
			i272 = i
		}
	}
	require.Greater(t, i26p, i26, "v1_26p must follow v1_26")
	require.Greater(t, i27, i26p, "v1_27 must follow v1_26p")
	require.Greater(t, i271, i27, "v1_27_1 must follow v1_27")
	require.Greater(t, i272, i271, "v1_27_2 must follow v1_27_1")
}
