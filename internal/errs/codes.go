// SPDX-License-Identifier: MIT

package errs

// Stable machine-oriented codes for JSON error.code and scripts.
const (
	CodeInvalidFormat          = "invalid_format"
	CodeInvalidInput           = "invalid_input"
	CodeInvalidAddress         = "invalid_address"
	CodeInvalidFlagCombination = "invalid_flag_combination"
	CodeInvalidConfig          = "invalid_config"
	// A command that changes how a device runs was not confirmed with --yes-i-am-sure.
	CodeConfirmationRequired = "confirmation_required"

	// The UDP socket cannot be opened.
	CodeTransportBindFailed = "transport_bind_failed"
	// Another program holds the local UDP port (usually 47808).
	CodeTransportPortInUse = "transport_port_in_use"
	// A host name does not resolve to an IPv4 address.
	CodeTransportResolveFailed = "transport_resolve_failed"
	// A datagram cannot be sent (no route, network down).
	CodeTransportSendFailed = "transport_send_failed"
	// The BBMD did not accept the foreign device registration.
	CodeForeignDeviceFailed = "foreign_device_registration_failed"
	// The socket was closed while a request was pending.
	CodeConnectionLost = "connection_lost"

	// The device did not answer a request in time.
	CodeRequestTimeout = "request_timeout"
	// A request that changes the device was sent and not acknowledged: the device may or
	// may not have carried it out.
	CodeOutcomeUnknown = "outcome_unknown"

	CodeProtocolViolation = "protocol_violation"
	CodeMalformedResponse = "malformed_response"
	CodeAPDUTooLarge      = "apdu_too_large"
	CodeNoRoute           = "no_route"
	CodeUnsupported       = "unsupported"
	CodeBVLCResult        = "bvlc_result"

	// Prefixes of the codes of a Reject and an Abort: the reason name follows, as in
	// reject_unrecognized_service. The code of an Error is the BACnet error code itself,
	// as in unknown_object.
	CodeRejectPrefix = "reject_"
	CodeAbortPrefix  = "abort_"

	// Context was canceled (not invalid user input).
	CodeContextCanceled = "context_canceled"

	CodeJSONEncodeFailed       = "json_encode_failed"
	CodeTableRenderFailed      = "table_render_failed"
	CodeOutputWriteFailed      = "output_write_failed"
	CodeOutputFileWriteFailed  = "output_file_write_failed"
	CodeOutputFileCreateFailed = "output_file_create_failed"
	CodeInputFileOpenFailed    = "input_file_open_failed"
	CodeInputDecodeFailed      = "input_decode_failed"

	CodeInternalUnexpected = "internal_unexpected"
)
