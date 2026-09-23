package api

import "time"

const (
	RETRIES  = 5
	SLEEP_MS = 200
)

// Intel SGX status codes used by the S4 classifier. Retry BUSY/OUT_OF_TCS only.
const (
	SGXSuccess               uint32 = 0x00000000
	SGXErrorUnexpected       uint32 = 0x00000001
	SGXErrorInvalidParameter uint32 = 0x00000002
	SGXErrorOutOfTCS         uint32 = 0x00001003
	SGXErrorInvalidSignature uint32 = 0x00002003
	// 0x4001 is SGX_ERROR_SERVICE_UNAVAILABLE in Intel headers; kept as retryable.
	SGXErrorBusy             uint32 = 0x00004001
	// Intel SGX_ERROR_BUSY (TCS busy). Must retry or prevote-nil on live HW.
	SGXErrorBusyTCS          uint32 = 0x0000400A
)

func ShouldRetry(status uint32) bool {
	return status == SGXErrorBusy || status == SGXErrorBusyTCS || status == SGXErrorOutOfTCS
}

func Classify(op func() uint32) uint32 {
	return classify(op, false)
}

func ClassifyAndSleep(op func() uint32) uint32 {
	return classify(op, true)
}

func classify(op func() uint32, sleep bool) uint32 {
	var status uint32
	for i := 0; i <= RETRIES; i++ {
		status = op()
		if status == SGXSuccess || !ShouldRetry(status) {
			return status
		}
		if i == RETRIES {
			break
		}
		if sleep {
			time.Sleep(SLEEP_MS * time.Millisecond)
		}
	}
	return status
}
