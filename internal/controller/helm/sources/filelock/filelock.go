// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package filelock

// Package filelock provides utilities for file-based locking across processes.
// It wraps github.com/gofrs/flock to provide a clean callback-based API for
// protecting concurrent filesystem operations in multi-pod deployments.
import (
	"context"
	"fmt"
	"time"

	"github.com/gofrs/flock"
)

// WithLock executes the given operation while holding an exclusive file lock.
// The lock is automatically released when the operation completes or an error occurs.
//
// This is designed for protecting concurrent filesystem operations across multiple
// processes or pods sharing a ReadWriteMany PVC. The lock uses OS-level advisory
// file locking (flock) which is automatically released if the process crashes.
//
// Parameters:
//   - lockPath: Path to the lock file (will be created if it doesn't exist)
//   - timeout: How long to wait for the lock before giving up
//   - operation: Function to execute while holding the lock
//
// Returns error if lock acquisition fails or if the operation returns an error.
//
// Example:
//
//	lockPath := filepath.Join(cacheDir, "chart-postgresql-12.1.2.lock")
//	err := filelock.WithLock(lockPath, 30*time.Second, func() error {
//	    return downloadChart(...)
//	})
func WithLock(lockPath string, timeout time.Duration, operation func() error) error {
	fileLock := flock.New(lockPath)
	lockCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	locked, err := fileLock.TryLockContext(lockCtx, time.Second)
	if err == nil && locked {
		defer fileLock.Unlock()
	}
	if err != nil {
		return fmt.Errorf("failed to acquire file lock %s: %w", lockPath, err)
	}
	if !locked {
		return fmt.Errorf("failed to acquire file lock %s: timeout after %v", lockPath, timeout)
	}

	return operation()
}

// WithLockContext executes the given operation while holding an exclusive file lock,
// with support for context cancellation.
//
// This variant allows the operation to respect context cancellation from the caller,
// useful for operations that can be interrupted (e.g., HTTP requests, long-running tasks).
//
// Parameters:
//   - ctx: Context for cancellation (combined with timeout for lock acquisition)
//   - lockPath: Path to the lock file (will be created if it doesn't exist)
//   - timeout: How long to wait for the lock before giving up
//   - operation: Function to execute while holding the lock (receives context)
//
// Returns error if lock acquisition fails, context is cancelled, or operation returns error.
//
// Example:
//
//	err := filelock.WithLockContext(ctx, lockPath, 30*time.Second, func(ctx context.Context) error {
//	    return downloadWithContext(ctx, ...)
//	})
func WithLockContext(ctx context.Context, lockPath string, timeout time.Duration, operation func(context.Context) error) error {
	fileLock := flock.New(lockPath)
	lockCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	locked, err := fileLock.TryLockContext(lockCtx, time.Second)
	if err == nil && locked {
		defer fileLock.Unlock()
	}
	if err != nil {
		return fmt.Errorf("failed to acquire file lock %s: %w", lockPath, err)
	}
	if !locked {
		return fmt.Errorf("failed to acquire file lock %s: timeout after %v", lockPath, timeout)
	}

	return operation(ctx)
}
