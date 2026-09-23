//go:build sgx

package api

// #include <stdlib.h>
// #include "bindings.h"
import "C"

import (
	"fmt"
)

type i32 = C.int32_t

type (
	i64    = C.int64_t
	u64    = C.uint64_t
	u32    = C.uint32_t
	u8     = C.uint8_t
	u8_ptr = *C.uint8_t
	usize  = C.uintptr_t
	cint   = C.int
	cbool  = C.bool
)

type EnclaveRandom struct {
	Random []byte `json:"random"`
	Proof  []byte `json:"proof"`
}

func statusErr(op string, status uint32) error {
	return fmt.Errorf("%s: sgx status 0x%x", op, status)
}

func ValidateRandom(encryptedRandom EnclaveRandom, blockHash []byte, valsetHash []byte, height uint64) error {
	if len(valsetHash) != 32 {
		return fmt.Errorf("validate_random: valset hash must be 32 bytes")
	}
	randomSlice := sendSlice(encryptedRandom.Random)
	defer freeAfterSend(randomSlice)
	proofSlice := sendSlice(encryptedRandom.Proof)
	defer freeAfterSend(proofSlice)
	blockHashSlice := sendSlice(blockHash)
	defer freeAfterSend(blockHashSlice)
	valsetSlice := sendSlice(valsetHash)
	defer freeAfterSend(valsetSlice)

	status := ClassifyAndSleep(func() uint32 {
		return uint32(C.validate_random(randomSlice, proofSlice, blockHashSlice, valsetSlice, u64(height)))
	})
	if status != SGXSuccess {
		return statusErr("validate_random", status)
	}
	return nil
}

func GetRandom(blockHash []byte, height uint64) (*EnclaveRandom, error) {
	blockHashSlice := sendSlice(blockHash)
	defer freeAfterSend(blockHashSlice)

	var out C.Buffer
	status := ClassifyAndSleep(func() uint32 {
		errmsg := C.Buffer{}
		var buf C.Buffer
		st := uint32(C.get_random_number(blockHashSlice, u64(height), &buf, &errmsg))
		if st == SGXSuccess {
			out = buf
		}
		return st
	})
	if status != SGXSuccess {
		return nil, statusErr("get_random", status)
	}

	vec := receiveVector(out)
	if len(vec) != 80 {
		return nil, fmt.Errorf("got random from enclave with a weird length: %d", len(vec))
	}
	return &EnclaveRandom{
		Random: vec[0:48],
		Proof:  vec[48:80],
	}, nil
}

func SubmitValidatorSet(valSet []byte, height uint64) error {
	valSetSlice := sendSlice(valSet)
	defer freeAfterSend(valSetSlice)

	status := ClassifyAndSleep(func() uint32 {
		errmsg := C.Buffer{}
		return uint32(C.submit_next_validator_set(valSetSlice, u64(height), &errmsg))
	})
	if status != SGXSuccess {
		return statusErr("submit_validator_set", status)
	}
	return nil
}

func SetScheduledTxs(marshaledData []byte) error {
	errmsg := C.Buffer{}
	dataSlice := sendSlice(marshaledData)
	defer freeAfterSend(dataSlice)

	status := ClassifyAndSleep(func() uint32 {
		errmsg = C.Buffer{}
		C.set_scheduled_txs(dataSlice, &errmsg)
		if errmsg.len == 0 {
			return SGXSuccess
		}
		return SGXErrorUnexpected
	})
	if status != SGXSuccess {
		return fmt.Errorf("failed setting scheduled txs in enclave")
	}
	return nil
}

func GetScheduledTxs() ([]byte, error) {
	var out C.Buffer
	status := ClassifyAndSleep(func() uint32 {
		errmsg := C.Buffer{}
		buf := C.get_scheduled_txs(&errmsg)
		if errmsg.len == 0 {
			out = buf
			return SGXSuccess
		}
		return SGXErrorUnexpected
	})
	if status != SGXSuccess {
		return nil, fmt.Errorf("failed to get scheduled txs from enclave")
	}
	return receiveVector(out), nil
}
