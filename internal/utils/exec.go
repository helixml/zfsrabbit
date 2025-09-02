package utils

import (
	"context"
	"os/exec"
	"time"
)

// TimeoutExecutor wraps exec.Cmd with timeout functionality
type TimeoutExecutor struct {
	defaultTimeout time.Duration
}

// NewTimeoutExecutor creates a new timeout executor with a default timeout
func NewTimeoutExecutor(timeout time.Duration) *TimeoutExecutor {
	return &TimeoutExecutor{
		defaultTimeout: timeout,
	}
}

// CommandWithTimeout creates a command with a timeout context
func (e *TimeoutExecutor) CommandWithTimeout(ctx context.Context, name string, args ...string) *exec.Cmd {
	if ctx == nil {
		ctx, _ = context.WithTimeout(context.Background(), e.defaultTimeout)
	}
	return exec.CommandContext(ctx, name, args...)
}

// Command creates a command with the default timeout
func (e *TimeoutExecutor) Command(name string, args ...string) *exec.Cmd {
	ctx, _ := context.WithTimeout(context.Background(), e.defaultTimeout)
	return exec.CommandContext(ctx, name, args...)
}

// RunWithTimeout runs a command with timeout and returns error
func (e *TimeoutExecutor) RunWithTimeout(ctx context.Context, name string, args ...string) error {
	cmd := e.CommandWithTimeout(ctx, name, args...)
	return cmd.Run()
}

// OutputWithTimeout runs a command and returns output with timeout
func (e *TimeoutExecutor) OutputWithTimeout(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := e.CommandWithTimeout(ctx, name, args...)
	return cmd.Output()
}
