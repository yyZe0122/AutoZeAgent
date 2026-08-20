package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/yyZe0122/yunmengze-agent/internal/tools/internal/executor"
)

func decodeStrict(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func encodeResult(value any) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode tool result: %w", err)
	}
	return encoded, nil
}

func encodeProcessResult(result executor.Result, runErr error) (json.RawMessage, error) {
	if runErr == nil {
		return encodeResult(result)
	}
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		encoded, err := encodeResult(result)
		if err != nil {
			return nil, err
		}
		return encoded, runErr
	}
	return encodeResult(map[string]any{
		"error":            "tool_failed",
		"command":          result.Command,
		"arguments":        result.Arguments,
		"directory":        result.Directory,
		"exit_code":        result.ExitCode,
		"stdout":           result.Stdout,
		"stderr":           result.Stderr,
		"stdout_truncated": result.StdoutTruncated,
		"stderr_truncated": result.StderrTruncated,
		"message":          runErr.Error(),
		"hint":             "Read exit_code, stdout, and stderr. Do not claim the command succeeded.",
	})
}
