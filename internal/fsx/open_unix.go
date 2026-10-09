//go:build !windows

package fsx

import "syscall"

const openFlags = syscall.O_NONBLOCK
