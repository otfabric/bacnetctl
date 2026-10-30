// SPDX-License-Identifier: MIT

package bac

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// snake turns a BACnet name into the form of an error code: unknown-object becomes
// unknown_object.
func snake(name string) string { return strings.ReplaceAll(name, "-", "_") }

// WrapError classifies an error of go-bacnet as a typed CLI error with a stable kind and
// code. A typed error is returned as it is.
func WrapError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errs.As(err); ok {
		return err
	}
	// A write that was sent and not answered: say exactly that, whatever the cause.
	var unknown *bacnet.OutcomeUnknownError
	if errors.As(err, &unknown) {
		why := "no acknowledgement arrived in time"
		if errors.Is(err, context.Canceled) {
			why = "it was interrupted before the acknowledgement arrived"
		} else if errors.Is(err, bacnet.ErrClosed) {
			why = "the socket was closed before the acknowledgement arrived"
		}
		return errs.New(errs.KindTimeout, errs.CodeOutcomeUnknown, fmt.Sprintf(
			"outcome unknown: the %s request was sent, but %s. The device may or may not have carried it out; "+
				"it was not sent again. Read the value back before you repeat it", unknown.Operation, why), err)
	}
	var (
		wpm    *service.WritePropertyMultipleError
		remote *bacnet.ErrorResponse
		reject *bacnet.RejectError
		abort  *bacnet.AbortError
		large  *bacnet.APDUTooLargeError
		bvlcOp *client.BVLCOperationError
		bvlcRe *client.BVLCResultError
	)
	switch {
	case errors.As(err, &wpm):
		e := remoteError(wpm.Class, wpm.Code, err)
		e.Message = fmt.Sprintf("%s (first failed write: %s %s)", e.Message, ObjectName(wpm.FirstFailed), PropertyName(wpm.FirstProperty.Identifier))
		return e
	case errors.As(err, &remote):
		return remoteError(remote.Class, remote.Code, err)
	case errors.As(err, &reject):
		name, id := bacnet.RejectReasonName(reject.Reason), reject.Reason
		e := errs.New(errs.KindDevice, errs.CodeRejectPrefix+snake(name),
			fmt.Sprintf("the device rejected the request: %s%s", name, rejectHint(reject.Reason)), err)
		e.Remote = &errs.Remote{PDU: "reject", Reason: name, ReasonID: &id}
		return e
	case errors.As(err, &abort):
		name, id := bacnet.AbortReasonName(abort.Reason), abort.Reason
		e := errs.New(errs.KindDevice, errs.CodeAbortPrefix+snake(name), fmt.Sprintf("the device aborted the request: %s", name), err)
		e.Remote = &errs.Remote{PDU: "abort", Reason: name, ReasonID: &id}
		return e
	case errors.Is(err, context.Canceled):
		return errs.New(errs.KindInterrupted, errs.CodeContextCanceled, "interrupted", err)
	case errors.Is(err, bacnet.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return errs.New(errs.KindTimeout, errs.CodeRequestTimeout, "timeout: no answer from the device", err)
	case errors.Is(err, bacnet.ErrClosed):
		return errs.New(errs.KindTransport, errs.CodeConnectionLost, "the BACnet/IP socket was closed", err)
	case errors.Is(err, bacnet.ErrInvalidConfig):
		return errs.InvalidInput(errs.CodeInvalidConfig, fmt.Sprintf("invalid input: %v", err), err)
	case errors.As(err, &large):
		return errs.New(errs.KindProtocol, errs.CodeAPDUTooLarge, fmt.Sprintf(
			"the request (%d octets) is larger than the device accepts (%d) and it does not take segmented requests",
			large.EncodedSize, large.RemoteMax), err)
	case errors.Is(err, client.ErrNoRoute):
		return errs.New(errs.KindProtocol, errs.CodeNoRoute, "no BACnet router to that network is known", err)
	case errors.As(err, &bvlcOp):
		return errs.New(errs.KindProtocol, errs.CodeBVLCResult, fmt.Sprintf("the BBMD refused %s: BVLC result 0x%04X", bvlcOp.Operation, bvlcOp.Code), err)
	case errors.As(err, &bvlcRe):
		return errs.New(errs.KindProtocol, errs.CodeBVLCResult, fmt.Sprintf("the BBMD refused: BVLC result 0x%04X", bvlcRe.Code), err)
	case errors.Is(err, bacnet.ErrMalformed), errors.Is(err, bacnet.ErrTrailingData), errors.Is(err, bacnet.ErrLimitExceeded):
		return errs.New(errs.KindProtocol, errs.CodeMalformedResponse, fmt.Sprintf("protocol error: %v", err), err)
	case errors.Is(err, bacnet.ErrProtocolViolation):
		return errs.New(errs.KindProtocol, errs.CodeProtocolViolation, fmt.Sprintf("protocol error: %v", err), err)
	case errors.Is(err, bacnet.ErrUnsupported), errors.Is(err, bacnet.ErrAPDUTooLarge):
		return errs.New(errs.KindUnsupported, errs.CodeUnsupported, fmt.Sprintf("protocol error: %v", err), err)
	}
	var netErr net.Error
	var opErr *net.OpError
	if errors.As(err, &netErr) || errors.As(err, &opErr) {
		return errs.New(errs.KindTransport, errs.CodeTransportSendFailed, fmt.Sprintf("network error: %v", err), err)
	}
	return errs.Internal(err)
}

func remoteError(class, code uint16, cause error) *errs.Error {
	className, codeName := bacnet.ErrorClassName(class), bacnet.ErrorCodeName(code)
	e := errs.New(errs.KindDevice, snake(codeName), fmt.Sprintf("the device refused the request: %s (error class %s)", codeName, className), cause)
	e.Remote = &errs.Remote{PDU: "error", Class: className, ClassID: &class, Code: codeName, CodeID: &code}
	return e
}

func rejectHint(reason uint8) string {
	if reason == bacnet.RejectReasonUnrecognizedService {
		return " (it does not implement this service)"
	}
	return ""
}

// EmbeddedErrorInfo renders err as the error object of a row or record in a result.
func EmbeddedErrorInfo(err error) *types.ErrorInfo {
	if err == nil {
		return nil
	}
	if e, ok := errs.As(WrapError(err)); ok {
		return &types.ErrorInfo{Kind: string(e.Kind), Code: e.Code, Message: e.Error(), BACnet: e.Remote}
	}
	return &types.ErrorInfo{Kind: string(errs.KindInternal), Code: errs.CodeInternalUnexpected, Message: err.Error()}
}

// PropertyErrorInfo renders the error of one property in a result. The message is short:
// a row has no room for more than the name of the error.
func PropertyErrorInfo(err error) *types.ErrorInfo {
	info := EmbeddedErrorInfo(err)
	if info != nil && info.BACnet != nil && info.BACnet.PDU == "error" {
		info.Message = info.BACnet.Code
	}
	return info
}

// NotImplemented reports whether err says that the device does not implement the service
// that was asked for, so that another way to the same result is worth trying.
func NotImplemented(err error) bool {
	var reject *bacnet.RejectError
	if errors.As(err, &reject) {
		return reject.Reason == bacnet.RejectReasonUnrecognizedService
	}
	var remote *bacnet.ErrorResponse
	if errors.As(err, &remote) {
		return remote.Class == bacnet.ErrorClassServices &&
			(remote.Code == bacnet.ErrorCodeRejectUnrecognizedService || remote.Code == bacnet.ErrorCodeOptionalFunctionalityNotSupported ||
				remote.Code == bacnet.ErrorCodeServiceRequestDenied)
	}
	return false
}

// Fatal reports whether err ends a command that works through a list: the device is gone
// or the command was interrupted. An answer of the device, even a refusal, is not fatal.
func Fatal(err error) bool {
	if err == nil {
		return false
	}
	var (
		remote *bacnet.ErrorResponse
		reject *bacnet.RejectError
		abort  *bacnet.AbortError
	)
	return !errors.As(err, &remote) && !errors.As(err, &reject) && !errors.As(err, &abort)
}
