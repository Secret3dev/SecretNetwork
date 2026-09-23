package compute_test

import (
	"errors"
	"testing"

	"github.com/scrtlabs/SecretNetwork/x/compute/internal/keeper"
	"github.com/stretchr/testify/require"
)

func TestApplyApprovedMachineIDsReturnsErrOnEcallFail(t *testing.T) {
	called := 0
	err := keeper.ApplyApprovedMachineIDs([][]byte{{0x01}, {0x02}}, func(_ []byte) error {
		called++
		if called == 2 {
			return errors.New("ecall failed")
		}
		return nil
	}, nil)
	require.Error(t, err)
	require.Equal(t, 2, called)
}

func TestApplyApprovedMachineIDsSuccess(t *testing.T) {
	var persisted [][]byte
	err := keeper.ApplyApprovedMachineIDs([][]byte{{0xaa}}, func(_ []byte) error {
		return nil
	}, func(id []byte) {
		persisted = append(persisted, id)
	})
	require.NoError(t, err)
	require.Equal(t, [][]byte{{0xaa}}, persisted)
}
