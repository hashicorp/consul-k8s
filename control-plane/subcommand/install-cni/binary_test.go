// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package installcni

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/hashicorp/go-hclog"
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

// writeTestNftBundle creates a bundle laid out like the one produced by bundle-nft.sh.
func writeTestNftBundle(t *testing.T, dir, nftContent string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ld.so"), []byte("loader"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "nft"), []byte(nftContent), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib", "libnftables.so.1"), []byte("lib"), 0o644))
}

func bundleVersions(t *testing.T, dir string) []string {
	t.Helper()
	versions, err := filepath.Glob(filepath.Join(dir, nftBundleVersionPrefix+"*"))
	require.NoError(t, err)
	for i := range versions {
		versions[i] = filepath.Base(versions[i])
	}
	return versions
}

func TestInstallNftBundle(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	writeTestNftBundle(t, src, "v1")

	require.NoError(t, installNftBundle(src, dest))

	link := filepath.Join(dest, consulNftName)
	target, err := os.Readlink(link)
	require.NoError(t, err)
	require.Equal(t, []string{target}, bundleVersions(t, dest))
	require.False(t, filepath.IsAbs(target), "symlink target should be relative")

	for name, perm := range map[string]os.FileMode{"ld.so": 0o755, "nft": 0o755, "lib/libnftables.so.1": 0o644} {
		info, err := os.Stat(filepath.Join(link, name))
		require.NoError(t, err, name)
		require.Equal(t, perm, info.Mode().Perm(), name)
	}
	content, err := os.ReadFile(filepath.Join(link, "nft"))
	require.NoError(t, err)
	require.Equal(t, "v1", string(content))

	// No temporary files are left behind.
	leftovers, err := filepath.Glob(filepath.Join(dest, nftBundleTempPrefix+"*"))
	require.NoError(t, err)
	require.Empty(t, leftovers)
}

func TestInstallNftBundle_Idempotent(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	writeTestNftBundle(t, src, "v1")

	require.NoError(t, installNftBundle(src, dest))
	first, err := os.Readlink(filepath.Join(dest, consulNftName))
	require.NoError(t, err)
	require.NoError(t, installNftBundle(src, dest))
	second, err := os.Readlink(filepath.Join(dest, consulNftName))
	require.NoError(t, err)

	require.Equal(t, first, second)
	require.Equal(t, []string{first}, bundleVersions(t, dest))
}

func TestInstallNftBundle_UpgradeKeepsPreviousVersion(t *testing.T) {
	dest := t.TempDir()
	link := filepath.Join(dest, consulNftName)
	var targets []string
	for _, v := range []string{"v1", "v2", "v3"} {
		src := t.TempDir()
		writeTestNftBundle(t, src, v)
		require.NoError(t, installNftBundle(src, dest))
		target, err := os.Readlink(link)
		require.NoError(t, err)
		targets = append(targets, target)

		content, err := os.ReadFile(filepath.Join(link, "nft"))
		require.NoError(t, err)
		require.Equal(t, v, string(content))
	}

	// v1 is pruned, v2 is kept as the previous version for in-flight plugin invocations.
	require.ElementsMatch(t, []string{targets[1], targets[2]}, bundleVersions(t, dest))
}

func TestInstallNftBundle_ReplacesNonSymlink(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	writeTestNftBundle(t, src, "v1")
	require.NoError(t, os.WriteFile(filepath.Join(dest, consulNftName), []byte("stale file"), 0o755))

	require.NoError(t, installNftBundle(src, dest))
	info, err := os.Lstat(filepath.Join(dest, consulNftName))
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink)
}

func TestInstallNftBundle_RejectsUnsupportedFiles(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	writeTestNftBundle(t, src, "v1")
	require.NoError(t, os.Symlink("nft", filepath.Join(src, "link")))

	require.ErrorContains(t, installNftBundle(src, dest), "unsupported file type")
	_, err := os.Lstat(filepath.Join(dest, consulNftName))
	require.True(t, os.IsNotExist(err))
}

func TestRemoveNftBundle(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	writeTestNftBundle(t, src, "v1")
	require.NoError(t, installNftBundle(src, dest))
	other := filepath.Join(dest, "other-plugin")
	require.NoError(t, os.WriteFile(other, []byte("x"), 0o755))

	require.NoError(t, removeNftBundle(dest))

	_, err := os.Lstat(filepath.Join(dest, consulNftName))
	require.True(t, os.IsNotExist(err))
	require.Empty(t, bundleVersions(t, dest))
	_, err = os.Stat(other)
	require.NoError(t, err, "unrelated files must not be removed")

	// Removing again is a no-op.
	require.NoError(t, removeNftBundle(dest))
}

func TestCommand_InstallNftBundleMissingSource(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	cmd := &Command{flagCNIBinSourceDir: src, logger: hclog.NewNullLogger()}

	cmd.installNftBundle(dest)

	_, err := os.Lstat(filepath.Join(dest, consulNftName))
	require.True(t, os.IsNotExist(err))
}
