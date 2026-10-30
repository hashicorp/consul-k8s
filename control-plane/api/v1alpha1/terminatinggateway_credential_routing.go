// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"net"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

// Credential modes for spec.services[].credential.mode.
const (
	CredentialModeInject = "inject"
	CredentialModeNone   = "none"
)

// These mirror Consul Enterprise's terminating-gateway credential validation so
// an invalid resource is rejected at admission rather than failing to sync.
var (
	credentialBindingIDRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	credentialDNSLabelRegexp  = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
)

// ValidateCredentialRouting validates the credential-injection routing that
// ToConsul writes to the Consul config entry and returns a single aggregated
// error. The controller uses it to refuse deploying a credential-injection
// workload whose routing Consul would reject.
func (in *TerminatingGateway) ValidateCredentialRouting() error {
	errs := in.validateCredentialRouting(field.NewPath("spec"))
	if len(errs) == 0 {
		return nil
	}
	return errs.ToAggregate()
}

func (in *TerminatingGateway) validateCredentialRouting(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	servicesPath := path.Child("services")

	if !in.CredentialRoutingEnabled() {
		for i, svc := range in.Spec.Services {
			if svc.Credential != nil {
				errs = append(errs, field.Invalid(servicesPath.Index(i).Child("credential"), svc.Credential,
					"credential requires spec.credentialInjection or spec.deployment.credentialInjection.enabled"))
			}
		}
		return errs
	}

	if r := in.Spec.CredentialInjection; r != nil {
		routingPath := path.Child("credentialInjection")
		if r.UDSPath != "" {
			if !filepath.IsAbs(r.UDSPath) {
				errs = append(errs, field.Invalid(routingPath.Child("udsPath"), r.UDSPath, "udsPath must be an absolute path"))
			} else if ci := in.Spec.Deployment.CredentialInjection; ci != nil && ci.Enabled && r.UDSPath != DefaultCredentialInjectionUDSPath {
				errs = append(errs, field.Invalid(routingPath.Child("udsPath"), r.UDSPath,
					"udsPath must be "+DefaultCredentialInjectionUDSPath+" (the processor socket mounted in the pod) when spec.deployment.credentialInjection is enabled"))
			}
		}
		if r.MessageTimeout != "" {
			if d, err := time.ParseDuration(r.MessageTimeout); err != nil || d <= 0 {
				errs = append(errs, field.Invalid(routingPath.Child("messageTimeout"), r.MessageTimeout, "messageTimeout must be a positive duration"))
			}
		}
	}

	seenBindingIDs := make(map[string]struct{})
	for i, svc := range in.Spec.Services {
		svcPath := servicesPath.Index(i)
		if svc.Name == WildcardSpecifier {
			errs = append(errs, field.Invalid(svcPath.Child("name"), svc.Name, "wildcard service name is not supported with credential injection"))
			continue
		}
		if svc.Credential == nil {
			errs = append(errs, field.Required(svcPath.Child("credential"), "credential.mode is required for every linked service when credential injection is enabled"))
			continue
		}
		credPath := svcPath.Child("credential")
		switch svc.Credential.Mode {
		case CredentialModeInject:
			if !credentialBindingIDRegexp.MatchString(svc.Credential.BindingID) {
				errs = append(errs, field.Invalid(credPath.Child("bindingID"), svc.Credential.BindingID,
					"bindingID must match "+credentialBindingIDRegexp.String()))
			} else if _, dup := seenBindingIDs[svc.Credential.BindingID]; dup {
				errs = append(errs, field.Duplicate(credPath.Child("bindingID"), svc.Credential.BindingID))
			} else {
				seenBindingIDs[svc.Credential.BindingID] = struct{}{}
			}
			if svc.CAFile == "" {
				errs = append(errs, field.Required(svcPath.Child("caFile"), "caFile is required when credential.mode is inject"))
			}
			if !isCredentialDNSName(svc.SNI) {
				errs = append(errs, field.Invalid(svcPath.Child("sni"), svc.SNI, "sni must be a DNS name (not an IP address) when credential.mode is inject"))
			}
		case CredentialModeNone:
			if svc.Credential.BindingID != "" {
				errs = append(errs, field.Invalid(credPath.Child("bindingID"), svc.Credential.BindingID, "bindingID must be empty when credential.mode is none"))
			}
		default:
			errs = append(errs, field.NotSupported(credPath.Child("mode"), svc.Credential.Mode, []string{CredentialModeInject, CredentialModeNone}))
		}
	}
	return errs
}

func isCredentialDNSName(name string) bool {
	name = strings.TrimSuffix(name, ".")
	if name == "" || len(name) > 253 || net.ParseIP(name) != nil {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if !credentialDNSLabelRegexp.MatchString(label) {
			return false
		}
	}
	return true
}
