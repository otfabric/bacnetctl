// SPDX-License-Identifier: MIT

package types

import "github.com/otfabric/bacnetctl/internal/errs"

// ErrorInfo is the structured error object for JSON output and embedded per-row failures.
type ErrorInfo struct {
	Kind    string `json:"kind"`
	Code    string `json:"code"`
	Message string `json:"message"`
	// BACnet is the refusal of the device, when the error is its Error, Reject or Abort.
	BACnet *errs.Remote `json:"bacnet,omitempty"`
}

// ErrorEnvelope is the top-level JSON shape for fatal errors (--format json).
type ErrorEnvelope struct {
	Error ErrorInfo `json:"error"`
}
