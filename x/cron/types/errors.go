package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

// x/cron module sentinel errors
var (
	ErrSample         = errors.Register(ModuleName, 1100, "sample error")
	ErrMissingCronKey = errors.Register(ModuleName, 1101, "cron secp256k1 key missing")
)
