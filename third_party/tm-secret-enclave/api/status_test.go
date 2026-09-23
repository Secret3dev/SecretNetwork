package api

import "testing"

func TestShouldRetryBusyAndOutOfTCSOnly(t *testing.T) {
	if !ShouldRetry(SGXErrorBusy) {
		t.Fatal("BUSY must retry")
	}
	if !ShouldRetry(SGXErrorOutOfTCS) {
		t.Fatal("OUT_OF_TCS must retry")
	}
	if !ShouldRetry(SGXErrorBusyTCS) {
		t.Fatal("Intel SGX_ERROR_BUSY 0x400A must retry")
	}
	for _, st := range []uint32{
		SGXSuccess,
		SGXErrorUnexpected,
		SGXErrorInvalidParameter,
		SGXErrorInvalidSignature,
		0xdeadbeef,
	} {
		if ShouldRetry(st) {
			t.Fatalf("status 0x%x must not retry", st)
		}
	}
}

func TestClassifyStopsImmediatelyOnInvalidSignature(t *testing.T) {
	calls := 0
	st := Classify(func() uint32 {
		calls++
		return SGXErrorInvalidSignature
	})
	if st != SGXErrorInvalidSignature {
		t.Fatalf("got 0x%x", st)
	}
	if calls != 1 {
		t.Fatalf("crypto fail retried: calls=%d", calls)
	}
}

func TestClassifyRetriesOutOfTCSThenSuccess(t *testing.T) {
	calls := 0
	st := Classify(func() uint32 {
		calls++
		if calls < 3 {
			return SGXErrorOutOfTCS
		}
		return SGXSuccess
	})
	if st != SGXSuccess {
		t.Fatalf("got 0x%x", st)
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestClassifyRetriesBusyThenSuccess(t *testing.T) {
	calls := 0
	st := Classify(func() uint32 {
		calls++
		if calls < 3 {
			return SGXErrorBusy
		}
		return SGXSuccess
	})
	if st != SGXSuccess {
		t.Fatalf("got 0x%x", st)
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
}
