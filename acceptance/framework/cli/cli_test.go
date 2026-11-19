// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/stretchr/testify/require"
)

func TestRunKubernetesOptions(t *testing.T) {
	binDirectory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(binDirectory, cliBinaryName), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0700))
	t.Setenv("PATH", binDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, tc := range []struct {
		name      string
		openShift bool
		args      []string
		want      []string
	}{
		{name: "namespace from options", openShift: true, args: []string{"proxy", "list"}, want: []string{"proxy", "list", "-kubeconfig", "/test/config", "-context", "test-context", "-namespace", "consul"}},
		{name: "explicit namespace", openShift: true, args: []string{"proxy", "list", "-namespace", "apps"}, want: []string{"proxy", "list", "-namespace", "apps", "-kubeconfig", "/test/config", "-context", "test-context"}},
		{name: "explicit equals namespace", openShift: true, args: []string{"proxy", "list", "-namespace=apps"}, want: []string{"proxy", "list", "-namespace=apps", "-kubeconfig", "/test/config", "-context", "test-context"}},
		{name: "install namespace", openShift: true, args: []string{"install"}, want: []string{"install", "-kubeconfig", "/test/config", "-context", "test-context", "-namespace", "consul"}},
		{name: "uninstall namespace", openShift: true, args: []string{"uninstall"}, want: []string{"uninstall", "-kubeconfig", "/test/config", "-context", "test-context", "-namespace", "consul"}},
		{name: "troubleshoot namespace", openShift: true, args: []string{"troubleshoot", "upstreams"}, want: []string{"troubleshoot", "upstreams", "-kubeconfig", "/test/config", "-context", "test-context", "-namespace", "consul"}},
		{name: "upgrade discovers namespace", openShift: true, args: []string{"upgrade", "-auto-approve"}, want: []string{"upgrade", "-auto-approve", "-kubeconfig", "/test/config", "-context", "test-context"}},
		{name: "unknown command", openShift: true, args: []string{"unknown"}, want: []string{"unknown", "-kubeconfig", "/test/config", "-context", "test-context"}},
		{name: "non-OpenShift leaves namespace unset", args: []string{"proxy", "list"}, want: []string{"proxy", "list", "-kubeconfig", "/test/config", "-context", "test-context"}},
		{name: "non-OpenShift install", args: []string{"install"}, want: []string{"install", "-kubeconfig", "/test/config", "-context", "test-context"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cli, err := NewCLI()
			require.NoError(t, err)
			cli.OpenShift = tc.openShift
			output, err := cli.Run(t, &k8s.KubectlOptions{ConfigPath: "/test/config", ContextName: "test-context", Namespace: "consul"}, tc.args...)
			require.NoError(t, err)
			require.Equal(t, tc.want, strings.Split(strings.TrimSpace(string(output)), "\n"))
		})
	}
}

func TestRunErrorOutput(t *testing.T) {
	for _, tc := range []struct {
		name      string
		openShift bool
		status    string
		want      string
	}{
		{name: "success preserves stdout", openShift: true, status: "0", want: "stdout\n"},
		{name: "failure includes stderr", openShift: true, status: "1", want: "stdout\nstderr\n"},
		{name: "non-OpenShift failure returns stdout", status: "1", want: "stdout\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binDirectory := t.TempDir()
			script := "#!/bin/sh\necho stdout\necho stderr >&2\nexit " + tc.status + "\n"
			require.NoError(t, os.WriteFile(filepath.Join(binDirectory, cliBinaryName), []byte(script), 0700))
			t.Setenv("PATH", binDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
			cli, err := NewCLI()
			require.NoError(t, err)
			cli.OpenShift = tc.openShift
			output, err := cli.Run(t, &k8s.KubectlOptions{}, "proxy", "list")
			if tc.status == "0" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, tc.want, string(output))
		})
	}
}
