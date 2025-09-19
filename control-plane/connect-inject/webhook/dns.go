// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"fmt"
	"strconv"

	"github.com/hashicorp/consul/agent/netutil"
	"github.com/miekg/dns"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

const (
	// These defaults are taken from the /etc/resolv.conf man page
	// and are used by the dns library.
	defaultDNSOptionNdots    = 1
	defaultDNSOptionTimeout  = 5
	defaultDNSOptionAttempts = 2

	// defaultEtcResolvConfFile is the default location of the /etc/resolv.conf file.
	defaultEtcResolvConfFile = "/etc/resolv.conf"
)

func (w *MeshWebhook) configureDNS(pod *corev1.Pod, k8sNS string) error {
	// First, we need to determine the nameservers configured in this cluster from /etc/resolv.conf.
	etcResolvConf := defaultEtcResolvConfFile
	if w.etcResolvFile != "" {
		etcResolvConf = w.etcResolvFile
	}
	cfg, err := dns.ClientConfigFromFile(etcResolvConf)
	if err != nil {
		return err
	}

	// Set DNS policy on the pod to None because we want DNS to work according to the config we will provide.
	pod.Spec.DNSPolicy = corev1.DNSNone

	// Set the consul-dataplane's DNS server as the first server in the list (i.e. localhost).
	// We want to do that so that when consul cannot resolve the record, we will fall back to the nameservers
	// configured in our /etc/resolv.conf. It's important to add Consul DNS as the first nameserver because
	// if we put kube DNS first, it will return NXDOMAIN response and a DNS client will not fall back to other nameservers.

	nameserver := consulDataplaneDNSBindHost

	ds, err := netutil.IsDualStack(w.ConsulConfig.APIClientConfig, false)
	if err != nil {
		return fmt.Errorf("unable to get consul dual stack status with error: %s", err.Error())
	}
	if ds {
		nameserver = ipv6ConsulDataplaneDNSBindHost
	}

	if pod.Spec.DNSConfig == nil {
		nameservers := []string{nameserver}
		nameservers = append(nameservers, cfg.Servers...)
		var options []corev1.PodDNSConfigOption
		if cfg.Ndots != defaultDNSOptionNdots {
			ndots := strconv.Itoa(cfg.Ndots)
			options = append(options, corev1.PodDNSConfigOption{
				Name:  "ndots",
				Value: &ndots,
			})
		}
		if cfg.Timeout != defaultDNSOptionTimeout {
			options = append(options, corev1.PodDNSConfigOption{
				Name:  "timeout",
				Value: ptr.To(strconv.Itoa(cfg.Timeout)),
			})
		}
		if cfg.Attempts != defaultDNSOptionAttempts {
			options = append(options, corev1.PodDNSConfigOption{
				Name:  "attempts",
				Value: ptr.To(strconv.Itoa(cfg.Attempts)),
			})
		}

		// Replace release namespace in the searches with the pod namespace.
		// This is so that the searches we generate will be for the pod's namespace
		// instead of the namespace of the connect-injector. E.g. instead of
		// consul.svc.cluster.local it should be <pod ns>.svc.cluster.local.
		var searches []string
		// Kubernetes will add a search domain for <namespace>.svc.cluster.local so we can always
		// expect it to be there. See https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/#namespaces-of-services.
		consulReleaseNSSearchDomain := fmt.Sprintf("%s.svc.cluster.local", w.ReleaseNamespace)
		for _, search := range cfg.Search {
			if search == consulReleaseNSSearchDomain {
				searches = append(searches, fmt.Sprintf("%s.svc.cluster.local", k8sNS))
			} else {
				searches = append(searches, search)
			}
		}

		pod.Spec.DNSConfig = &corev1.PodDNSConfig{
			Nameservers: nameservers,
			Searches:    searches,
			Options:     options,
		}
	} else {
		return fmt.Errorf("DNS redirection to Consul is not supported with an already defined DNSConfig on the pod")
	}
	return nil
}
