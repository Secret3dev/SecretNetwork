package keeper

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	crontypes "github.com/scrtlabs/SecretNetwork/x/cron/types"
)

func TestGetModulePrivateKeyMissingFailClosed(t *testing.T) {
	t.Setenv(CronSecp256k1Env, "")
	_, err := GetModulePrivateKey()
	require.ErrorIs(t, err, crontypes.ErrMissingCronKey)
}

func TestGetModulePrivateKeyRejectsShort(t *testing.T) {
	t.Setenv(CronSecp256k1Env, base64.StdEncoding.EncodeToString([]byte("short")))
	_, err := GetModulePrivateKey()
	require.Error(t, err)
}

func TestGetModulePrivateKeyAccepts32(t *testing.T) {
	raw := make([]byte, 32)
	raw[0] = 1
	t.Setenv(CronSecp256k1Env, base64.StdEncoding.EncodeToString(raw))
	key, err := GetModulePrivateKey()
	require.NoError(t, err)
	require.Equal(t, raw, key.Bytes())
}
