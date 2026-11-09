// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package installcni

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// copyFile copies a file from a source directory to a destination directory.
func copyFile(srcFile, destDir string) error {
	// If the src file does not exist then either the incorrect command line argument was used or
	// the docker container we built is broken somehow.
	if _, err := os.Stat(srcFile); os.IsNotExist(err) {
		return err
	}

	filename := filepath.Base(srcFile)
	// If the destDir does not exist then the incorrect command line argument was used or
	// the CNI settings for the kubelet are not correct.
	info, err := os.Stat(destDir)
	if os.IsNotExist(err) {
		return err
	}

	if !info.IsDir() {
		return fmt.Errorf("destination directory %s is not a directory", destDir)
	}

	// Check if the user bit is enabled in file permission.
	if info.Mode().Perm()&(1<<(uint(7))) == 0 {
		return fmt.Errorf("cannot write to destination directory %s", destDir)
	}

	srcBytes, err := os.ReadFile(srcFile)
	if err != nil {
		return fmt.Errorf("could not read %s file", srcFile)
	}

	err = os.WriteFile(filepath.Join(destDir, filename), srcBytes, info.Mode())
	if err != nil {
		return fmt.Errorf("error copying %s binary to %s", filename, destDir)
	}
	return nil
}

func removeFile(path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Nothing to delete.
		return nil
	}

	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("error removing file %s: %w", path, err)
	}
	return nil
}

// The nft bundle is installed on the node as <cniBinDir>/.consul-nft-v-<hash>/ and exposed through the
// <cniBinDir>/consul-nft symlink. Swapping the symlink with a rename is atomic, so the CNI plugin always
// sees a complete bundle, including while a new version is being installed.
const (
	nftBundleVersionPrefix = ".consul-nft-v-"
	nftBundleTempPrefix    = ".consul-nft-tmp-"
)

// installNftBundle installs the nft bundle from the image into destDir. A missing bundle is not fatal:
// the plugin still works on nodes that have nft installed.
func (c *Command) installNftBundle(destDir string) {
	srcDir := filepath.Join(c.flagCNIBinSourceDir, consulNftName)
	if info, err := os.Stat(srcDir); err != nil || !info.IsDir() {
		c.logger.Warn("nft bundle not found in image, nodes without nft will not be able to apply traffic redirection rules",
			"source", srcDir, "error", err)
		return
	}
	c.logger.Info("Installing nft bundle", "destination", destDir)
	if err := installNftBundle(srcDir, destDir); err != nil {
		c.logger.Error("Unable to install nft bundle", "error", err)
	}
}

// installNftBundle copies srcDir into a content-addressed directory in destDir, points the consul-nft
// symlink at it and removes bundle versions other than the current and previous one. The previous one is
// kept so a plugin invocation that resolved the old symlink can finish.
func installNftBundle(srcDir, destDir string) error {
	hash, err := hashDir(srcDir)
	if err != nil {
		return fmt.Errorf("could not hash %s: %w", srcDir, err)
	}
	versionName := nftBundleVersionPrefix + hash
	versionDir := filepath.Join(destDir, versionName)

	if _, err := os.Stat(versionDir); os.IsNotExist(err) {
		tmpDir, err := os.MkdirTemp(destDir, nftBundleTempPrefix)
		if err != nil {
			return fmt.Errorf("could not create temp dir in %s: %w", destDir, err)
		}
		defer os.RemoveAll(tmpDir)
		if err := copyDir(srcDir, tmpDir); err != nil {
			return err
		}
		if err := os.Rename(tmpDir, versionDir); err != nil && !isExistErr(err) {
			return fmt.Errorf("could not rename %s to %s: %w", tmpDir, versionDir, err)
		}
	} else if err != nil {
		return err
	}

	link := filepath.Join(destDir, consulNftName)
	previous, _ := os.Readlink(link)
	if info, err := os.Lstat(link); err == nil && info.Mode()&os.ModeSymlink == 0 {
		// Not a symlink; it cannot be replaced atomically by rename.
		if err := os.RemoveAll(link); err != nil {
			return fmt.Errorf("could not remove %s: %w", link, err)
		}
	}
	tmpLink := filepath.Join(destDir, nftBundleTempPrefix+"link-"+hash)
	_ = os.Remove(tmpLink)
	// The relative target resolves both on the host and inside the installer container.
	if err := os.Symlink(versionName, tmpLink); err != nil {
		return fmt.Errorf("could not create symlink %s: %w", tmpLink, err)
	}
	if err := os.Rename(tmpLink, link); err != nil {
		_ = os.Remove(tmpLink)
		return fmt.Errorf("could not rename %s to %s: %w", tmpLink, link, err)
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", destDir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, nftBundleVersionPrefix) && name != versionName && name != previous {
			if err := os.RemoveAll(filepath.Join(destDir, name)); err != nil {
				return fmt.Errorf("could not remove old nft bundle %s: %w", name, err)
			}
		}
	}
	return nil
}

// removeNftBundle removes the consul-nft symlink and all installed bundle versions from destDir.
func removeNftBundle(destDir string) error {
	if err := removeFile(filepath.Join(destDir, consulNftName)); err != nil {
		return err
	}
	entries, err := os.ReadDir(destDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), nftBundleVersionPrefix) || strings.HasPrefix(e.Name(), nftBundleTempPrefix) {
			if err := os.RemoveAll(filepath.Join(destDir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// hashDir returns a short hash of the relative paths, permissions and contents of the regular files in dir.
func hashDir(dir string) (string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			files = append(files, path)
		} else if !d.IsDir() {
			return fmt.Errorf("unsupported file type in nft bundle: %s", path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)

	h := sha256.New()
	for _, path := range files {
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%o\x00", filepath.ToSlash(rel), info.Mode().Perm())
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// copyDir copies the regular files and directories in src to dst, preserving permissions.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			return os.Chmod(target, info.Mode().Perm())
		case d.Type().IsRegular():
			return copyRegularFile(path, target, info.Mode().Perm())
		default:
			return fmt.Errorf("unsupported file type in nft bundle: %s", path)
		}
	})
}

func copyRegularFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("could not copy %s to %s: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, perm)
}

// isExistErr reports whether err means the rename target already exists, e.g. because another installer
// pod installed the same bundle version concurrently.
func isExistErr(err error) bool {
	return errors.Is(err, fs.ErrExist) || errors.Is(err, syscall.ENOTEMPTY)
}
