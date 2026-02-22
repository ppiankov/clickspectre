package main

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestClassifyError_Nil(t *testing.T) {
	if got := classifyError(nil); got != ExitSuccess {
		t.Errorf("classifyError(nil) = %d, want %d", got, ExitSuccess)
	}
}

func TestClassifyError_Findings(t *testing.T) {
	err := &FindingsError{Count: 5}
	if got := classifyError(err); got != ExitFindings {
		t.Errorf("classifyError(FindingsError) = %d, want %d", got, ExitFindings)
	}
}

func TestClassifyError_FindingsWrapped(t *testing.T) {
	err := fmt.Errorf("audit: %w", &FindingsError{Count: 3})
	if got := classifyError(err); got != ExitFindings {
		t.Errorf("classifyError(wrapped FindingsError) = %d, want %d", got, ExitFindings)
	}
}

func TestClassifyError_NotFound(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"os.ErrNotExist", os.ErrNotExist},
		{"wrapped os.ErrNotExist", fmt.Errorf("open: %w", os.ErrNotExist)},
		{"not a directory", errors.New("repo path is not a directory")},
		{"does not exist", errors.New("path does not exist")},
		{"no such file", errors.New("no such file or directory")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyError(tc.err); got != ExitNotFound {
				t.Errorf("classifyError(%q) = %d, want %d", tc.err, got, ExitNotFound)
			}
		})
	}
}

func TestClassifyError_Network(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"dial", errors.New("dial tcp: connection refused")},
		{"connection refused", errors.New("connection refused")},
		{"i/o timeout", errors.New("i/o timeout")},
		{"network unreachable", errors.New("network is unreachable")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyError(tc.err); got != ExitNetwork {
				t.Errorf("classifyError(%q) = %d, want %d", tc.err, got, ExitNetwork)
			}
		})
	}
}

func TestClassifyError_InvalidArg(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"required", errors.New("clickhouse-dsn is required")},
		{"invalid", errors.New("invalid output format")},
		{"must be", errors.New("timeout must be greater than zero")},
		{"expected", errors.New("expected json, sarif, or text")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyError(tc.err); got != ExitInvalidArg {
				t.Errorf("classifyError(%q) = %d, want %d", tc.err, got, ExitInvalidArg)
			}
		})
	}
}

func TestClassifyError_Internal(t *testing.T) {
	err := errors.New("something went wrong")
	if got := classifyError(err); got != ExitInternal {
		t.Errorf("classifyError(%q) = %d, want %d", err, got, ExitInternal)
	}
}

func TestFindingsError_Error(t *testing.T) {
	err := &FindingsError{Count: 7}
	want := "7 findings detected"
	if got := err.Error(); got != want {
		t.Errorf("FindingsError.Error() = %q, want %q", got, want)
	}
}
