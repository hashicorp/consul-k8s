// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	capi "github.com/hashicorp/consul/api"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// This file contains structs that are shared between multiple config entries.

// metaValueMaxLength is the maximum allowed string length of a metadata value.
const metaValueMaxLength = 512

type MeshGatewayMode string

// Expose describes HTTP paths to expose through Envoy outside of Connect.
// Users can expose individual paths and/or all HTTP/GRPC paths for checks.
type Expose struct {
	// Checks defines whether paths associated with Consul checks will be exposed.
	// This flag triggers exposing all HTTP and GRPC check paths registered for the service.
	Checks bool `json:"checks,omitempty"`

	// Paths is the list of paths exposed through the proxy.
	Paths []ExposePath `json:"paths,omitempty"`
}

type ExposePath struct {
	// ListenerPort defines the port of the proxy's listener for exposed paths.
	ListenerPort int `json:"listenerPort,omitempty"`

	// Path is the path to expose through the proxy, ie. "/metrics".
	Path string `json:"path,omitempty"`

	// LocalPathPort is the port that the service is listening on for the given path.
	LocalPathPort int `json:"localPathPort,omitempty"`

	// Protocol describes the upstream's service protocol.
	// Valid values are "http" and "http2", defaults to "http".
	Protocol string `json:"protocol,omitempty"`
}

type TransparentProxy struct {
	// OutboundListenerPort is the port of the listener where outbound application
	// traffic is being redirected to.
	OutboundListenerPort int `json:"outboundListenerPort,omitempty"`

	// DialedDirectly indicates whether transparent proxies can dial this proxy instance directly.
	// The discovery chain is not considered when dialing a service instance directly.
	// This setting is useful when addressing stateful services, such as a database cluster with a leader node.
	DialedDirectly bool `json:"dialedDirectly,omitempty"`
}

type MutualTLSMode string

const (
	// MutualTLSModeDefault represents no specific mode and should
	// be used to indicate that a different layer of the configuration
	// chain should take precedence.
	MutualTLSModeDefault MutualTLSMode = ""

	// MutualTLSModeStrict requires mTLS for incoming traffic.
	MutualTLSModeStrict MutualTLSMode = "strict"

	// MutualTLSModePermissive allows incoming non-mTLS traffic.
	MutualTLSModePermissive MutualTLSMode = "permissive"
)

func (m MutualTLSMode) validate() error {
	switch m {
	case MutualTLSModeDefault, MutualTLSModeStrict, MutualTLSModePermissive:
		return nil
	}
	return fmt.Errorf("Must be one of %q, %q, or %q.",
		MutualTLSModeDefault, MutualTLSModeStrict, MutualTLSModePermissive,
	)
}

func (m MutualTLSMode) toConsul() capi.MutualTLSMode {
	return capi.MutualTLSMode(m)
}

// MeshGateway controls how Mesh Gateways are used for upstream Connect
// services.
type MeshGateway struct {
	// Mode is the mode that should be used for the upstream connection.
	// One of none, local, or remote.
	Mode string `json:"mode,omitempty"`
}

type ProxyMode string

// HTTPHeaderModifiers is a set of rules for HTTP header modification that
// should be performed by proxies as the request passes through them. It can
// operate on either request or response headers depending on the context in
// which it is used.
type HTTPHeaderModifiers struct {
	// Add is a set of name -> value pairs that should be appended to the request
	// or response (i.e. allowing duplicates if the same header already exists).
	Add map[string]string `json:"add,omitempty"`

	// Set is a set of name -> value pairs that should be added to the request or
	// response, overwriting any existing header values of the same name.
	Set map[string]string `json:"set,omitempty"`

	// Remove is the set of header names that should be stripped from the request
	// or response.
	Remove []string `json:"remove,omitempty"`
}

// EnvoyExtension has configuration for an extension that patches Envoy resources.
type EnvoyExtension struct {
	Name     string `json:"name,omitempty"`
	Required bool   `json:"required,omitempty"`
	// +kubebuilder:validation:Type=object
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// EnvoyExtensions represents a list of the EnvoyExtension configuration.
type EnvoyExtensions []EnvoyExtension

func (in MeshGateway) toConsul() capi.MeshGatewayConfig {
	mode := capi.MeshGatewayMode(in.Mode)
	switch mode {
	case capi.MeshGatewayModeLocal, capi.MeshGatewayModeRemote, capi.MeshGatewayModeNone:
		return capi.MeshGatewayConfig{
			Mode: mode,
		}
	default:
		return capi.MeshGatewayConfig{
			Mode: capi.MeshGatewayModeDefault,
		}
	}
}

func (in MeshGateway) validate(path *field.Path) *field.Error {
	modes := []string{"remote", "local", "none", ""}
	if !sliceContains(modes, in.Mode) {
		return field.Invalid(path.Child("mode"), in.Mode, notInSliceMessage(modes))
	}
	return nil
}

func (in Expose) toConsul() capi.ExposeConfig {
	var paths []capi.ExposePath
	for _, path := range in.Paths {
		paths = append(paths, capi.ExposePath{
			ListenerPort:  path.ListenerPort,
			Path:          path.Path,
			LocalPathPort: path.LocalPathPort,
			Protocol:      path.Protocol,
		})
	}
	return capi.ExposeConfig{
		Checks: in.Checks,
		Paths:  paths,
	}
}

func (in Expose) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	protocols := []string{"http", "http2"}
	for i, pathCfg := range in.Paths {
		indexPath := path.Child("paths").Index(i)
		if invalidPathPrefix(pathCfg.Path) {
			errs = append(errs, field.Invalid(
				indexPath.Child("path"),
				pathCfg.Path,
				`must begin with a '/'`))
		}
		if pathCfg.Protocol != "" && !sliceContains(protocols, pathCfg.Protocol) {
			errs = append(errs, field.Invalid(
				indexPath.Child("protocol"),
				pathCfg.Protocol,
				notInSliceMessage(protocols)))
		}
	}
	return errs
}

func (in *TransparentProxy) toConsul() *capi.TransparentProxyConfig {
	if in == nil {
		return nil
	}
	return &capi.TransparentProxyConfig{
		OutboundListenerPort: in.OutboundListenerPort,
		DialedDirectly:       in.DialedDirectly,
	}
}

func (in *TransparentProxy) validate(path *field.Path) *field.Error {
	if in == nil {
		return nil
	}
	if in.OutboundListenerPort != 0 {
		return field.Invalid(path.Child("outboundListenerPort"), in.OutboundListenerPort, "use the annotation `consul.hashicorp.com/transparent-proxy-outbound-listener-port` to configure the Outbound Listener Port")
	}
	return nil
}

func (in *ProxyMode) validate(path *field.Path) *field.Error {
	if in != nil {
		return field.Invalid(path, in, "use the annotation `consul.hashicorp.com/transparent-proxy` to configure the Transparent Proxy Mode")
	}
	return nil
}

func (in *HTTPHeaderModifiers) toConsul() *capi.HTTPHeaderModifiers {
	if in == nil {
		return nil
	}
	return &capi.HTTPHeaderModifiers{
		Add:    in.Add,
		Set:    in.Set,
		Remove: in.Remove,
	}
}

func (in EnvoyExtensions) toConsul() []capi.EnvoyExtension {
	if in == nil {
		return nil
	}

	outConfig := make([]capi.EnvoyExtension, 0)

	for _, e := range in {
		consulExtension := capi.EnvoyExtension{
			Name:     e.Name,
			Required: e.Required,
		}

		// We already validate that arguments is present
		var args map[string]interface{}
		_ = json.Unmarshal(e.Arguments, &args)
		consulExtension.Arguments = args
		outConfig = append(outConfig, consulExtension)
	}

	return outConfig
}

func (in EnvoyExtensions) validate(path *field.Path) field.ErrorList {
	if len(in) == 0 {
		return nil
	}

	var errs field.ErrorList
	for i, e := range in {
		if err := e.validate(path.Child("envoyExtension").Index(i)); err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

func (in EnvoyExtension) validate(path *field.Path) *field.Error {
	// Validate that the arguments are not nil
	if in.Arguments == nil {
		err := field.Required(path.Child("arguments"), "arguments must be defined")
		return err
	}
	// Validate that the arguments are valid json
	var outConfig map[string]interface{}
	if err := json.Unmarshal(in.Arguments, &outConfig); err != nil {
		return field.Invalid(path.Child("arguments"), string(in.Arguments), fmt.Sprintf(`must be valid map value: %s`, err))
	}
	return nil
}

// FailoverPolicy specifies the exact mechanism used for failover.
type FailoverPolicy struct {
	// Mode specifies the type of failover that will be performed. Valid values are
	// "sequential", "" (equivalent to "sequential") and "order-by-locality".
	Mode string `json:"mode,omitempty"`
	// Regions is the ordered list of the regions of the failover targets.
	// Valid values can be "us-west-1", "us-west-2", and so on.
	Regions []string `json:"regions,omitempty"`
}

func (in *FailoverPolicy) toConsul() *capi.ServiceResolverFailoverPolicy {
	if in == nil {
		return nil
	}

	return &capi.ServiceResolverFailoverPolicy{
		Mode:    in.Mode,
		Regions: in.Regions,
	}
}

func (in *FailoverPolicy) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if in == nil {
		return nil
	}
	modes := []string{"", "sequential", "order-by-locality"}
	if !sliceContains(modes, in.Mode) {
		errs = append(errs, field.Invalid(path.Child("mode"), in.Mode, notInSliceMessage(modes)))
	}
	return errs
}

// PrioritizeByLocality controls whether the locality of services within the
// local partition will be used to prioritize connectivity.
type PrioritizeByLocality struct {
	// Mode specifies the type of prioritization that will be performed
	// when selecting nodes in the local partition.
	// Valid values are: "" (default "none"), "none", and "failover".
	Mode string `json:"mode,omitempty"`
}

func (in *PrioritizeByLocality) toConsul() *capi.ServiceResolverPrioritizeByLocality {
	if in == nil {
		return nil
	}

	return &capi.ServiceResolverPrioritizeByLocality{
		Mode: in.Mode,
	}
}

func (in *PrioritizeByLocality) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if in == nil {
		return nil
	}
	modes := []string{"", "none", "failover"}
	if !sliceContains(modes, in.Mode) {
		errs = append(errs, field.Invalid(path.Child("mode"), in.Mode, notInSliceMessage(modes)))
	}
	return errs
}

func notInSliceMessage(slice []string) string {
	return fmt.Sprintf(`must be one of "%s"`, strings.Join(slice, `", "`))
}

func sliceContains(slice []string, entry string) bool {
	for _, s := range slice {
		if entry == s {
			return true
		}
	}
	return false
}

func invalidPathPrefix(path string) bool {
	return path != "" && !strings.HasPrefix(path, "/")
}

func meta(datacenter string) map[string]string {
	return map[string]string{
		common.SourceKey:     common.SourceValue,
		common.DatacenterKey: datacenter,
	}
}

// transparentProxyConfigComparer compares two TransparentProxyConfig pointers.
// It returns whether they are equal but will treat an empty struct and a nil
// pointer as equal. This is needed to fix a bug in the Consul API in Consul
// 1.10.0 (https://github.com/hashicorp/consul/issues/10595) where Consul will
// always return the empty struct for the TransparentProxy key even if it was
// written as a nil pointer. With the default comparator, a nil pointer and
// empty struct are treated as different and so we would always treat the
// CRD as not synced and would continually try and write it to Consul.
func transparentProxyConfigComparer(a, b *capi.TransparentProxyConfig) bool {
	empty := capi.TransparentProxyConfig{}

	// If one is a nil pointer and the other is the empty struct
	// then treat them as equal.
	if a == nil && b != nil && *b == empty {
		return true
	}
	if b == nil && a != nil && *a == empty {
		return true
	}

	// Otherwise compare as normal.
	return cmp.Equal(a, b)
}
