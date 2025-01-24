// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhookv2

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

func TestMeshWebhook_configureDNS(t *testing.T) {
	cases := map[string]struct {
		etcResolv    string
		expDNSConfig *corev1.PodDNSConfig
	}{
		"empty /etc/resolv.conf file": {
			expDNSConfig: &corev1.PodDNSConfig{
				Nameservers: []string{"127.0.0.1"},
			},
		},
		"one nameserver": {
			etcResolv: `nameserver 1.1.1.1`,
			expDNSConfig: &corev1.PodDNSConfig{
				Nameservers: []string{"127.0.0.1", "1.1.1.1"},
			},
		},
		"mutiple nameservers, searches, and options": {
			etcResolv: `
nameserver 1.1.1.1
nameserver 2.2.2.2
search foo.bar bar.baz
options ndots:5 timeout:6 attempts:3`,
			expDNSConfig: &corev1.PodDNSConfig{
				Nameservers: []string{"127.0.0.1", "1.1.1.1", "2.2.2.2"},
				Searches:    []string{"foo.bar", "bar.baz"},
				Options: []corev1.PodDNSConfigOption{
					{
						Name:  "ndots",
						Value: ptr.To("5"),
					},
					{
						Name:  "timeout",
						Value: ptr.To("6"),
					},
					{
						Name:  "attempts",
						Value: ptr.To("3"),
					},
				},
			},
		},
		"replaces release specific search domains": {
			etcResolv: `
nameserver 1.1.1.1
nameserver 2.2.2.2
search consul.svc.cluster.local svc.cluster.local cluster.local
options ndots:5`,
			expDNSConfig: &corev1.PodDNSConfig{
				Nameservers: []string{"127.0.0.1", "1.1.1.1", "2.2.2.2"},
				Searches:    []string{"default.svc.cluster.local", "svc.cluster.local", "cluster.local"},
				Options: []corev1.PodDNSConfigOption{
					{
						Name:  "ndots",
						Value: ptr.To("5"),
					},
				},
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			etcResolvFile, err := os.CreateTemp("", "")
			require.NoError(t, err)
			t.Cleanup(func() {
				_ = os.RemoveAll(etcResolvFile.Name())
			})
			_, err = etcResolvFile.WriteString(c.etcResolv)
			require.NoError(t, err)
			w := MeshWebhook{
				etcResolvFile:    etcResolvFile.Name(),
				ReleaseNamespace: "consul",
			}

			pod := minimal()
			err = w.configureDNS(pod, "default")
			require.NoError(t, err)
			require.Equal(t, corev1.DNSNone, pod.Spec.DNSPolicy)
			require.Equal(t, c.expDNSConfig, pod.Spec.DNSConfig)
		})
	}
}

func TestMeshWebhook_configureDNS_error(t *testing.T) {
	w := MeshWebhook{}

	pod := minimal()
	pod.Spec.DNSConfig = &corev1.PodDNSConfig{Nameservers: []string{"1.1.1.1"}}
	err := w.configureDNS(pod, "default")
	require.EqualError(t, err, "DNS redirection to Consul is not supported with an already defined DNSConfig on the pod")
}
