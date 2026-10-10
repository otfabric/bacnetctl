// SPDX-License-Identifier: MIT

package bac

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
)

// Directions of a file transfer in output.
const (
	DirectionDownload = "download"
	DirectionUpload   = "upload"
)

// Stdout is the value of --output that sends the content to standard output.
const Stdout = "-"

// maxChunk is the largest --chunk: more does not fit the largest BACnet/IP APDU.
const maxChunk = 1400

func checkFile(conn *config.ConnConfig, object string, chunk uint32) error {
	if err := CheckConn(conn); err != nil {
		return err
	}
	id, err := ParseObject(object)
	if err != nil {
		return err
	}
	if id.Type != bacnet.ObjectTypeFile {
		return fmt.Errorf("%s is not a file object: use file:INSTANCE", ObjectName(id))
	}
	if chunk == 0 || chunk > maxChunk {
		return fmt.Errorf("--chunk must be 1-%d octets", maxChunk)
	}
	return nil
}

// CheckFileGet validates the configuration of `file get`.
func CheckFileGet(cfg *config.FileGetConfig, object string) error {
	if err := checkFile(&cfg.ConnConfig, object, cfg.Chunk); err != nil {
		return err
	}
	if cfg.Output == "" {
		return fmt.Errorf("--output is required: the file to write, or - for stdout")
	}
	if cfg.MaxSize == 0 {
		return fmt.Errorf("--max-size must be at least 1")
	}
	return nil
}

// CheckFilePut validates the configuration of `file put`.
func CheckFilePut(cfg *config.FilePutConfig, object string) error {
	if err := checkFile(&cfg.ConnConfig, object, cfg.Chunk); err != nil {
		return err
	}
	if cfg.Input == "" {
		return fmt.Errorf("--input is required: the file to send")
	}
	if cfg.Start > 0x7FFFFFFF {
		return fmt.Errorf("--start is out of range")
	}
	return nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// FileGet reads a stream-access file object and writes its content to --output, or to
// stdout when that is "-".
func FileGet(ctx context.Context, cfg *config.FileGetConfig, objectArg string, stdout, logw io.Writer) (*types.FileResult, error) {
	object, _ := ParseObject(objectArg)
	s, err := Open(ctx, &cfg.SocketConfig, logw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	target, err := s.Target(ctx, &cfg.ConnConfig)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	// One octet more than allowed, so that a file of exactly --max-size is not refused.
	data, err := s.ReadFileStream(ctx, target, object, client.FileReadOptions{ChunkSize: cfg.Chunk, MaxTotal: int(cfg.MaxSize) + 1})
	if err != nil && len(data) <= int(cfg.MaxSize) {
		return nil, WrapError(err)
	}
	if len(data) > int(cfg.MaxSize) {
		return nil, errs.InvalidInput(errs.CodeInvalidInput,
			fmt.Sprintf("the file is larger than --max-size (%d octets): nothing was written", cfg.MaxSize), err)
	}
	res := &types.FileResult{
		Target: TargetText(target), Direction: DirectionDownload, ObjectRef: objRef(object), Length: len(data),
		SHA256: digest(data), Path: cfg.Output, DurationMS: ms(time.Since(start)),
	}
	if cfg.Output == Stdout {
		if _, err := stdout.Write(data); err != nil {
			return nil, errs.Output(errs.CodeOutputWriteFailed, err)
		}
		return res, nil
	}
	if err := os.WriteFile(cfg.Output, data, 0o644); err != nil { //nolint:gosec // a path the operator gave
		return nil, errs.New(errs.KindOutput, errs.CodeOutputFileWriteFailed, fmt.Sprintf("cannot write %s: %v", cfg.Output, err), err)
	}
	return res, nil
}

// FilePut writes the content of --input to a stream-access file object. Every request is
// sent once: when one is not acknowledged, the transfer stops there.
func FilePut(ctx context.Context, cfg *config.FilePutConfig, objectArg string, logw io.Writer) (*types.FileResult, error) {
	object, _ := ParseObject(objectArg)
	data, err := os.ReadFile(cfg.Input) //nolint:gosec // a path the operator gave
	if err != nil {
		return nil, errs.InvalidInput(errs.CodeInputFileOpenFailed, fmt.Sprintf("cannot read %s: %v", cfg.Input, err), err)
	}
	s, err := Open(ctx, &cfg.SocketConfig, logw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	target, err := s.Target(ctx, &cfg.ConnConfig)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	outcomes, err := s.WriteFileStream(ctx, target, object, int32(cfg.Start), data, client.FileWriteOptions{ChunkSize: cfg.Chunk})
	if err != nil {
		done := 0
		for _, o := range outcomes {
			if o.Err == nil {
				done += o.ByteCount
			}
		}
		e, _ := errs.As(WrapError(err))
		e.Message = fmt.Sprintf("%s (%d of %d octets were acknowledged before that)", e.Message, done, len(data))
		return nil, e
	}
	return &types.FileResult{
		Target: TargetText(target), Direction: DirectionUpload, ObjectRef: objRef(object), Length: len(data),
		SHA256: digest(data), Path: cfg.Input, Requests: len(outcomes), DurationMS: ms(time.Since(start)),
	}, nil
}
