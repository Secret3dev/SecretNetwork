//go:build !sgx

package api

import (
	"fmt"
	"math/rand"
)

type EnclaveRandom struct {
	Random []byte `json:"random"`
	Proof  []byte `json:"proof"`
}

func GetRandom(blockHash []byte, height uint64) (*EnclaveRandom, error) {
	buf := make([]byte, 32)
	_, err := rand.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to generate random")
	}

	return &EnclaveRandom{
		Random: buf,
		Proof:  nil,
	}, nil
}

func SubmitValidatorSet(valSet []byte, height uint64) error {
	return nil
}

func ValidateRandom(rand EnclaveRandom, blockHash []byte, valsetHash []byte, height uint64) error {
	if len(valsetHash) != 32 {
		return fmt.Errorf("validate_random: valset hash must be 32 bytes")
	}
	return nil
}

func GetScheduledTxs() ([]byte, error) {
	return nil, nil
}

func SetScheduledTxs(txs []byte) error {
	return nil
}
