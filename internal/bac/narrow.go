// SPDX-License-Identifier: MIT

package bac

import (
	"fmt"
	"math"

	bacnet "github.com/otfabric/go-bacnet"
)

// Range-checked conversions. A device can encode an Unsigned in up to eight octets, and a
// count or position can come from a flag: a value that does not fit is refused, never
// reduced to its low bits.

// u32 returns u when it fits 32 bits.
func u32(u uint64, what string) (uint32, error) {
	if u > math.MaxUint32 {
		return 0, fmt.Errorf("%w: %s %d exceeds 32 bits", bacnet.ErrMalformed, what, u)
	}
	return uint32(u), nil
}

// i32 returns v as a BACnet Signed32 count or position, which the flag checks keep at
// 2^31-1 or less.
func i32(v uint32, what string) (int32, error) {
	if v > math.MaxInt32 {
		return 0, fmt.Errorf("%s %d exceeds %d", what, v, math.MaxInt32)
	}
	return int32(v), nil
}

// octet returns v as a date or time field, or 0xFF (unspecified) when it does not fit.
func octet(v int) uint8 {
	if v < 0 || v > math.MaxUint8 {
		return 0xFF
	}
	return uint8(v)
}

// reached reports whether n results reach a limit; a limit of 0 is no limit.
func reached(n int, limit uint32) bool {
	if limit == 0 || n < 0 {
		return false
	}
	return uint64(n) >= uint64(limit)
}
