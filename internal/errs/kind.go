// SPDX-License-Identifier: MIT

package errs

// Kind is a coarse failure category for exit-code mapping and JSON error.kind.
type Kind string

const (
	KindUsage        Kind = "usage"
	KindInvalidInput Kind = "invalid_input"
	KindTransport    Kind = "transport"
	KindTimeout      Kind = "timeout"
	KindProtocol     Kind = "protocol"
	// KindDevice is a request the device answered and refused: a BACnet Error, Reject or
	// Abort.
	KindDevice   Kind = "device_error"
	KindOutput   Kind = "output"
	KindInternal Kind = "internal"
)
