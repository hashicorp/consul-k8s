// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package installcni

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCopyFile(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		srcFile     func() string // use any file
		dir         func() string
		expectedErr func(string, string) error
	}{
		{
			name: "source file does not exist",
			srcFile: func() string {
				return "doesnotexist"
			},
			dir: func() string {
				return ""
			},
			expectedErr: func(srcFile, _ string) error {
				return &os.PathError{Op: "stat", Path: srcFile, Err: syscall.ENOENT}
			},
		},
		{
			name: "destination directory does not exist",
			srcFile: func() string {
				return "testdata/10-kindnet.conflist" // any file will do
			},
			dir: func() string {
				return ""
			},
			expectedErr: func(_, destDir string) error {
				return &os.PathError{Op: "stat", Path: destDir, Err: syscall.ENOENT}
			},
		},
		{
			name: "destination is not a directory",
			srcFile: func() string {
				return "testdata/10-kindnet.conflist" // any file wil do
			},
			dir: func() string {
				return "testdata/10-kindnet.conflist"
			},

			expectedErr: func(_, destDir string) error {
				return fmt.Errorf("destination directory %s is not a directory", destDir)
			},
		},
		{
			name: "destination directory does not have write permissions",
			srcFile: func() string {
				return "testdata/10-kindnet.conflist"
			}, // any file wil do
			dir: func() string {
				tempDir := t.TempDir()
				os.Chmod(tempDir, 0555)
				return tempDir
			},
			expectedErr: func(_, destDir string) error {
				return fmt.Errorf("cannot write to destination directory %s", destDir)
			},
		},
		{
			name: "cannot read source file",
			srcFile: func() string {
				tempDir := t.TempDir()
				err := copyFile("testdata/10-kindnet.conflist", tempDir)
				require.NoError(t, err)
				filepath := filepath.Join(tempDir, "10-kindnet.conflist")
				os.Chmod(filepath, 0111)
				return filepath
			},
			dir: func() string {
				tempDir := t.TempDir()
				return tempDir
			},
			expectedErr: func(srcFile, _ string) error {
				return fmt.Errorf("could not read %s file", srcFile)
			},
		},
		{
			name: "copy file to dest directory",
			srcFile: func() string {
				return "testdata/10-kindnet.conflist"
			},
			dir: func() string {
				tempDir := t.TempDir()
				return tempDir
			},
			expectedErr: func(_, _ string) error {
				return nil
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			destDir := c.dir()
			srcFile := c.srcFile()
			actualErr := copyFile(srcFile, destDir)

			expErr := c.expectedErr(srcFile, destDir)

			require.Equal(t, expErr, actualErr)
		})
	}
}

func TestRemoveFile(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		srcFile func() string // use any file
	}{
		{
			name: "source file does not exist, nothing to remove, no error",
			srcFile: func() string {
				return "doesnotexist"
			},
		},
		{
			name: "can remove file",
			srcFile: func() string {
				tempDir := t.TempDir()
				err := copyFile("testdata/10-kindnet.conflist", tempDir)
				require.NoError(t, err)
				filepath := filepath.Join(tempDir, "10-kindnet.conflist")
				return filepath
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := removeFile(c.srcFile())
			require.NoError(t, err)
		})
	}
}
