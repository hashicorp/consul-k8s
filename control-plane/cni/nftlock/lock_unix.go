// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build !windows

// Package nftlock serializes access to the nft bundle that the CNI installer copies to the node. The CNI
// plugin holds a shared lock while it runs the bundled nft, and the installer holds an exclusive lock while
// it installs, prunes or removes bundle versions, so a version is never deleted while a plugin invocation
// is using it.
package nftlock

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// FileName is the name of the lock file in the CNI bin directory.
const FileName = ".consul-nft.lock"

const pollInterval = 10 * time.Millisecond

// Shared takes a shared lock on the bundle in dir. It returns an error matching fs.ErrNotExist if the lock
// file does not exist, which means that no bundle is installed in dir.
func Shared(dir string, timeout time.Duration) (release func(), err error) {
	return lock(dir, syscall.LOCK_SH, false, timeout)
}

// Exclusive takes an exclusive lock on the bundle in dir, creating the lock file if needed. The lock file
// may be removed by the holder of the exclusive lock.
func Exclusive(dir string, timeout time.Duration) (release func(), err error) {
	return lock(dir, syscall.LOCK_EX, true, timeout)
}

func lock(dir string, how int, create bool, timeout time.Duration) (func(), error) {
	path := filepath.Join(dir, FileName)
	flags := os.O_RDONLY
	if create {
		flags |= os.O_CREATE
	}
	deadline := time.Now().Add(timeout)
	for {
		f, err := os.OpenFile(path, flags, 0o644)
		if err != nil {
			return nil, err
		}
		if err := flock(f, how, deadline); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("could not lock %s: %w", path, err)
		}
		// The holder of the exclusive lock may have removed the lock file while we waited. A lock on a
		// removed file does not exclude anyone, so check that we locked the file that is at path now.
		current, err := os.Stat(path)
		if err == nil {
			var locked os.FileInfo
			if locked, err = f.Stat(); err == nil && os.SameFile(locked, current) {
				return func() { _ = f.Close() }, nil
			}
		}
		_ = f.Close()
		switch {
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return nil, err
		case errors.Is(err, fs.ErrNotExist) && !create:
			return nil, err
		case time.Now().After(deadline):
			return nil, fmt.Errorf("could not lock %s: timed out after %s", path, timeout)
		}
	}
}

func flock(f *os.File, how int, deadline time.Time) error {
	for {
		err := syscall.Flock(int(f.Fd()), how|syscall.LOCK_NB)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, syscall.EINTR):
			continue
		case !errors.Is(err, syscall.EWOULDBLOCK):
			return err
		case time.Now().After(deadline):
			return errors.New("timed out waiting for the lock")
		}
		time.Sleep(pollInterval)
	}
}
