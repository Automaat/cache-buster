package provider

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
	if len(args) == 0 {
		return CleanResult{}, nil
	}

	sizeBefore, beforeErr := sizeFn(ctx)

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Without a delay a grandchild holding the pipes keeps Run blocked after
	// the context kills the command.
	cmd.WaitDelay = cleanWaitDelay

	err := cmd.Run()
	output := strings.TrimSpace(stdout.String() + stderr.String())
	if err != nil {
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
