package v1_27

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpgradeNameAndEmptyStores(t *testing.T) {
	require.Equal(t, "v1.27.0", UpgradeName)
	require.Equal(t, "v1.27.0", Upgrade.UpgradeName)
	require.Empty(t, Upgrade.StoreUpgrades.Added)
	require.Empty(t, Upgrade.StoreUpgrades.Renamed)
	require.Empty(t, Upgrade.StoreUpgrades.Deleted)
}
