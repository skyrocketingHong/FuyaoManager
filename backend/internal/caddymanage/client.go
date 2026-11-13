package caddymanage

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"time"
)

func bytesReader(data []byte) *bytes.Reader { return bytes.NewReader(data) }

type envelope struct {
	Result *Result `json:"result,omitempty"`
	Error  string  `json:"error,omitempty"`
}
type limitedOutput struct{ bytes.Buffer }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxRequestBytes {
		return 0, ErrUnavailable
	}
	return b.Buffer.Write(p)
}

// Execute runs an operator-configured fixed capability with a typed stdin
// payload. HTTP can never replace argv, environment, stdin source or paths.
func Execute(ctx context.Context, argv []string, request Request) (Result, error) {
	if len(argv) == 0 {
		return Result{}, ErrUnavailable
	}
	data, err := json.Marshal(request)
	if err != nil {
		return Result{}, ErrInvalidRequest
	}
	if _, err := DecodeRequest(bytes.NewReader(data)); err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	command.Dir = "/"
	command.Stdin = bytes.NewReader(data)
	command.WaitDelay = 2 * time.Second
	var output limitedOutput
	command.Stdout = &output
	runErr := command.Run()
	var response envelope
	if strictJSON(output.Bytes(), &response) != nil {
		return Result{}, ErrUnavailable
	}
	if response.Error != "" {
		return Result{}, errorForCode(response.Error)
	}
	if runErr != nil || response.Result == nil {
		return Result{}, ErrUnavailable
	}
	return *response.Result, nil
}
func errorForCode(code string) error {
	switch code {
	case "not_registered":
		return ErrNotRegistered
	case "conflict":
		return ErrConflict
	case "validation":
		return ErrValidation
	case "permission":
		return ErrPermission
	case "unsupported":
		return ErrUnsupported
	case "invalid_request":
		return ErrInvalidRequest
	default:
		return ErrUnavailable
	}
}
func ErrorCode(err error) string {
	switch err {
	case ErrNotRegistered:
		return "not_registered"
	case ErrConflict:
		return "conflict"
	case ErrValidation:
		return "validation"
	case ErrPermission:
		return "permission"
	case ErrUnsupported:
		return "unsupported"
	case ErrInvalidRequest:
		return "invalid_request"
	default:
		return "unavailable"
	}
}
func WriteResponse(writer interface{ Write([]byte) (int, error) }, result Result, err error) error {
	value := envelope{Result: &result}
	if err != nil {
		value = envelope{Error: ErrorCode(err)}
	}
	data, e := json.Marshal(value)
	if e != nil {
		return e
	}
	_, e = writer.Write(append(data, '\n'))
	return e
}
