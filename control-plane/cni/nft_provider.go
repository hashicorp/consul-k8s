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

	"github.com/vishvananda/netns"
)

// bundledNftName is the directory that the CNI installer copies next to consul-cni. It contains the nft
// binary from the image's distro package together with its shared libraries (lib/) and dynamic loader
// (ld.so), and is used on nodes whose OS does not ship nft, e.g. GKE Container-Optimized OS.
const bundledNftName = "consul-nft"

// nftProvider implements nftables.Provider. It collects nft script lines and applies them atomically
// with `nft -f -` inside the pod's network namespace, using the node's nft binary when present and
// otherwise the bundled consul-nft directory.
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
}

func newNftProvider(netNS string, binDirs ...string) *nftProvider {
	return &nftProvider{
		netNS:      netNS,
		binDirs:    binDirs,
		lookPath:   exec.LookPath,
		runInNetNS: runInNetNS,
	}
}

// AddRule collects one nft script line. The first argument (the binary name) is ignored.
func (n *nftProvider) AddRule(_ string, args ...string) {
	n.lines = append(n.lines, strings.Join(args, " "))
}

// ApplyRules pipes all collected lines to `nft -f -` so they are applied atomically.
func (n *nftProvider) ApplyRules(_ string) error {
	nftCmd, err := n.nftCommand()
	if err != nil {
		return err
	}

	run := func() error {
		args := append(nftCmd[1:len(nftCmd):len(nftCmd)], "-f", "-")
		cmd := exec.Command(nftCmd[0], args...)
		cmd.Stdin = strings.NewReader(strings.Join(n.lines, "\n"))
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

// nftCommand returns the command used to run nft: the node's nft binary if it is in PATH, otherwise the
// bundled nft run through its bundled dynamic loader so that it only uses the bundled libraries.
func (n *nftProvider) nftCommand() ([]string, error) {
	if path, err := n.lookPath("nft"); err == nil {
		return []string{path}, nil
	}

	var searched []string
	for _, dir := range n.binDirs {
		if dir == "" {
			continue
		}
		bundle := filepath.Join(dir, bundledNftName)
		searched = append(searched, bundle)
		// Resolve the symlink once so that all paths refer to the same bundle version even if the
		// installer swaps the symlink to a new version while nft is running.
		resolved, err := filepath.EvalSymlinks(bundle)
		if err != nil {
			continue
		}
		loader, nft, lib := filepath.Join(resolved, "ld.so"), filepath.Join(resolved, "nft"), filepath.Join(resolved, "lib")
		if isExecutable(loader) && isExecutable(nft) && isDir(lib) {
			return []string{loader, "--library-path", lib, nft}, nil
		}
	}
	return nil, fmt.Errorf("nft binary not found in PATH and bundled %s not found (searched %s)",
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
	defer target.Close()

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
		defer orig.Close()
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
