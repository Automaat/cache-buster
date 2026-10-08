package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const cleanWaitDelay = 5 * time.Second

// runMeasuredClean runs args as a command, measuring cache size before and
// after via sizeFn to report freed bytes. name is used for warnings. It is the
// shared scaffold behind the command-based fullClean and Docker smartClean.
func runMeasuredClean(
	ctx context.Context,
	name string,
	args []string,
	sizeFn func(context.Context) (int64, error),
) (CleanResult, error) {
	return runMeasuredCleanTimeout(ctx, name, args, sizeFn, 0)
}

// runMeasuredCleanTimeout is runMeasuredClean with the command alone bounded
// by timeout (zero means unbounded); the size scans stay outside the budget.
// A timeout kills the command's whole process group.
func runMeasuredCleanTimeout(
	ctx context.Context,
	name string,
	args []string,
	sizeFn func(context.Context) (int64, error),
	timeout time.Duration,
) (CleanResult, error) {
	if len(args) == 0 {
		return CleanResult{}, nil
	}

	sizeBefore, beforeErr := sizeFn(ctx)

	cmdCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		cmdCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(cmdCtx, args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Without a delay a grandchild holding the pipes keeps Run blocked after
	// the context kills the command.
	cmd.WaitDelay = cleanWaitDelay

	err := cmd.Run()
	output := strings.TrimSpace(stdout.String() + stderr.String())
	if err != nil {
		if ctx.Err() == nil && errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("clean command timed out after %s: %w", timeout, err)
		}
		return CleanResult{Output: output}, err
	}

	sizeAfter, afterErr := sizeFn(ctx)
	// An unmeasured side would read as 0 and skew the freed bytes.
	var bytesCleaned int64
	if beforeErr == nil && afterErr == nil {
		bytesCleaned = sizeBefore - sizeAfter
	}
	if bytesCleaned < 0 {
		fmt.Fprintf(os.Stderr, "warning: %s cache size increased during clean\n", name)
		bytesCleaned = 0
	}

	return CleanResult{
		BytesCleaned: bytesCleaned,
		Output:       output,
	}, nil
}
