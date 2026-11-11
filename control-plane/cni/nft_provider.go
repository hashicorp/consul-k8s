// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/vishvananda/netns"

	"github.com/hashicorp/consul-k8s/control-plane/cni/nftlock"
)

// bundledNftName is the directory that the CNI installer copies next to consul-cni. It contains the nft
// binary from the image's distro package together with its shared libraries (lib/) and dynamic loader
// (ld.so), and is used on nodes whose OS does not ship nft, e.g. GKE Container-Optimized OS.
const bundledNftName = "consul-nft"

// nftProvider implements nftables.Provider. It collects nft script lines and applies them atomically
// with `nft -f <script file>` inside the pod's network namespace, using the bundled consul-nft directory
// when present and otherwise the node's nft binary.
type nftProvider struct {
	lines []string
	// netNS is the pod network namespace path. When empty the rules are applied in the current namespace.
	netNS string
	// binDirs are the directories searched for the bundled consul-nft directory.
	binDirs []string
	// lookPath finds a binary in PATH. It is overridable for testing.
	lookPath func(string) (string, error)
	// runInNetNS runs fn inside the network namespace at path. It is overridable for testing.
	runInNetNS func(path string, fn func() error) error
	// lockTimeout bounds the wait for the CNI installer to finish installing or removing the bundle.
	lockTimeout time.Duration
}

func newNftProvider(netNS string, binDirs ...string) *nftProvider {
	return &nftProvider{
		netNS:       netNS,
		binDirs:     binDirs,
		lookPath:    exec.LookPath,
		runInNetNS:  runInNetNS,
		lockTimeout: 10 * time.Second,
	}
}

// AddRule collects one nft script line. The first argument (the binary name) is ignored.
func (n *nftProvider) AddRule(_ string, args ...string) {
	n.lines = append(n.lines, strings.Join(args, " "))
}

// ApplyRules writes all collected lines to a script file and applies it with `nft -f <file>` so the rules
// are applied atomically. A file is used instead of stdin (`-f -`) because some node nft builds, e.g. on
// GKE Container-Optimized OS, do not support reading the script from stdin.
func (n *nftProvider) ApplyRules(_ string) error {
	nftCmd, release, err := n.nftCommand()
	if err != nil {
		return err
	}
	// Keep the bundle locked until nft has finished so the installer cannot remove it while it is in use.
	defer release()

	script, err := writeNftScript(n.lines)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(script) }()

	run := func() error {
		args := append(nftCmd[1:len(nftCmd):len(nftCmd)], "-f", script)
		cmd := exec.Command(nftCmd[0], args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to apply nftables rules with %s: %w, output: %s",
				strings.Join(nftCmd, " "), err, string(out))
		}
		return nil
	}

	if n.netNS == "" {
		return run()
	}
	return n.runInNetNS(n.netNS, run)
}

// Rules returns the collected nft commands prefixed with "nft ".
func (n *nftProvider) Rules() []string {
	rules := make([]string, 0, len(n.lines))
	for _, line := range n.lines {
		rules = append(rules, "nft "+line)
	}
	return rules
}

// ClearAllRules removes all collected rules.
func (n *nftProvider) ClearAllRules() {
	n.lines = nil
}

// writeNftScript writes lines to a new temporary file readable only by the current user and returns its path.
func writeNftScript(lines []string) (string, error) {
	f, err := os.CreateTemp("", "consul-cni-nft-*.nft")
	if err != nil {
		return "", fmt.Errorf("could not create nftables script file: %w", err)
	}
	_, err = f.WriteString(strings.Join(lines, "\n") + "\n")
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("could not write nftables script file: %w", err)
	}
	return f.Name(), nil
}

// nftCommand returns the command used to run nft: the bundled nft run through its bundled dynamic loader,
// so that a known nft version with its own libraries is used, or the node's nft binary from PATH if no
// bundle is installed. When the bundle is used it is locked (shared) until release is called.
func (n *nftProvider) nftCommand() (cmd []string, release func(), err error) {
	var searched []string
	for _, dir := range n.binDirs {
		if dir == "" {
			continue
		}
		bundle := filepath.Join(dir, bundledNftName)
		searched = append(searched, bundle)
		unlock, err := nftlock.Shared(dir, n.lockTimeout)
		if errors.Is(err, os.ErrNotExist) {
			// The installer creates the lock file before it installs a bundle.
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("could not lock bundled %s: %w", bundle, err)
		}
		// Resolve the symlink once so that all paths refer to the same bundle version even if the
		// installer swaps the symlink to a new version while nft is running.
		resolved, err := filepath.EvalSymlinks(bundle)
		if err == nil {
			loader, nft, lib := filepath.Join(resolved, "ld.so"), filepath.Join(resolved, "nft"), filepath.Join(resolved, "lib")
			if isExecutable(loader) && isExecutable(nft) && isDir(lib) {
				return []string{loader, "--library-path", lib, nft}, unlock, nil
			}
		}
		unlock()
	}

	if path, err := n.lookPath("nft"); err == nil {
		return []string{path}, func() {}, nil
	}
	return nil, nil, fmt.Errorf("nft binary not found in PATH and bundled %s not found (searched %s)",
		bundledNftName, strings.Join(searched, ", "))
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// bundledNftDirs returns the directories that may contain the bundled consul-nft directory: the configured CNI bin
// directory and the directory of the running consul-cni binary.
func bundledNftDirs(cniBinDir string) []string {
	dirs := []string{cniBinDir}
	if exe, err := os.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != cniBinDir {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// runInNetNS runs fn on an OS thread switched into the network namespace at path. Child processes started
// by fn inherit that namespace, so this does not depend on nsenter being installed on the node.
func runInNetNS(path string, fn func() error) error {
	target, err := netns.GetFromPath(path)
	if err != nil {
		return fmt.Errorf("could not open network namespace %s: %w", path, err)
	}
	defer func() { _ = target.Close() }()

	errCh := make(chan error, 1)
	go func() {
		// If the thread cannot be switched back to its original namespace it is left locked, so the Go
		// runtime terminates it when this goroutine exits instead of reusing it.
		runtime.LockOSThread()
		orig, err := netns.Get()
		if err != nil {
			runtime.UnlockOSThread()
			errCh <- fmt.Errorf("could not get current network namespace: %w", err)
			return
		}
		defer func() { _ = orig.Close() }()
		if err := netns.Set(target); err != nil {
			runtime.UnlockOSThread()
			errCh <- fmt.Errorf("could not enter network namespace %s: %w", path, err)
			return
		}
		fnErr := fn()
		if err := netns.Set(orig); err != nil {
			errCh <- errors.Join(fnErr, fmt.Errorf("could not restore network namespace: %w", err))
			return
		}
		runtime.UnlockOSThread()
		errCh <- fnErr
	}()
	return <-errCh
}
