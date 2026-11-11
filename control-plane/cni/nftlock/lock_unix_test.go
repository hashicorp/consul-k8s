// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build !windows

package nftlock

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestShared_NoLockFile(t *testing.T) {
	_, err := Shared(t.TempDir(), time.Second)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestExclusive_CreatesLockFile(t *testing.T) {
	dir := t.TempDir()
	release, err := Exclusive(dir, time.Second)
	require.NoError(t, err)
	release()
	_, err = os.Stat(filepath.Join(dir, FileName))
	require.NoError(t, err)
}

func TestExclusive_MissingDir(t *testing.T) {
	_, err := Exclusive(filepath.Join(t.TempDir(), "missing"), time.Second)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestSharedLocksDoNotConflict(t *testing.T) {
	dir := t.TempDir()
	createLockFile(t, dir)
	r1, err := Shared(dir, time.Second)
	require.NoError(t, err)
	defer r1()
	r2, err := Shared(dir, time.Second)
	require.NoError(t, err)
	r2()
}

func TestExclusiveWaitsForShared(t *testing.T) {
	dir := t.TempDir()
	createLockFile(t, dir)
	shared, err := Shared(dir, time.Second)
	require.NoError(t, err)

	_, err = Exclusive(dir, 50*time.Millisecond)
	require.ErrorContains(t, err, "timed out")

	acquired := make(chan error, 1)
	go func() {
		release, err := Exclusive(dir, 5*time.Second)
		if err == nil {
			release()
		}
		acquired <- err
	}()
	select {
	case <-acquired:
		t.Fatal("exclusive lock acquired while a shared lock is held")
	case <-time.After(100 * time.Millisecond):
	}
	shared()
	require.NoError(t, <-acquired)
}

func TestSharedWaitsForExclusive(t *testing.T) {
	dir := t.TempDir()
	exclusive, err := Exclusive(dir, time.Second)
	require.NoError(t, err)
	defer exclusive()

	_, err = Shared(dir, 50*time.Millisecond)
	require.ErrorContains(t, err, "timed out")
}

func TestSharedAfterLockFileRemoved(t *testing.T) {
	dir := t.TempDir()
	exclusive, err := Exclusive(dir, time.Second)
	require.NoError(t, err)

	result := make(chan error, 1)
	go func() {
		release, err := Shared(dir, 5*time.Second)
		if err == nil {
			release()
		}
		result <- err
	}()
	time.Sleep(50 * time.Millisecond)
	// The bundle is uninstalled: the lock file is removed while the exclusive lock is held.
	require.NoError(t, os.Remove(filepath.Join(dir, FileName)))
	exclusive()

	require.ErrorIs(t, <-result, fs.ErrNotExist)
}

func TestExclusiveAfterLockFileReplaced(t *testing.T) {
	dir := t.TempDir()
	exclusive, err := Exclusive(dir, time.Second)
	require.NoError(t, err)

	result := make(chan error, 1)
	go func() {
		release, err := Exclusive(dir, 5*time.Second)
		if err == nil {
			release()
		}
		result <- err
	}()
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.Remove(filepath.Join(dir, FileName)))
	exclusive()

	// The waiter retries on a new lock file instead of keeping a lock on the removed one.
	require.NoError(t, <-result)
	_, err = os.Stat(filepath.Join(dir, FileName))
	require.NoError(t, err)
}

func createLockFile(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, FileName), nil, 0o644))
}
