// +build !ignore_autogenerated

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"encoding/json"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Condition) DeepCopyInto(out *Condition) {
	*out = *in
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Condition.
func (in *Condition) DeepCopy() *Condition {
	if in == nil {
		return nil
	}
	out := new(Condition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in Conditions) DeepCopyInto(out *Conditions) {
	{
		in := &in
		*out = make(Conditions, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Conditions.
func (in Conditions) DeepCopy() Conditions {
	if in == nil {
		return nil
	}
	out := new(Conditions)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CookieConfig) DeepCopyInto(out *CookieConfig) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CookieConfig.
func (in *CookieConfig) DeepCopy() *CookieConfig {
	if in == nil {
		return nil
	}
	out := new(CookieConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Destination) DeepCopyInto(out *Destination) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Destination.
func (in *Destination) DeepCopy() *Destination {
	if in == nil {
		return nil
	}
	out := new(Destination)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ExposeConfig) DeepCopyInto(out *ExposeConfig) {
	*out = *in
	if in.Paths != nil {
		in, out := &in.Paths, &out.Paths
		*out = make([]ExposePath, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ExposeConfig.
func (in *ExposeConfig) DeepCopy() *ExposeConfig {
	if in == nil {
		return nil
	}
	out := new(ExposeConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ExposePath) DeepCopyInto(out *ExposePath) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ExposePath.
func (in *ExposePath) DeepCopy() *ExposePath {
	if in == nil {
		return nil
	}
	out := new(ExposePath)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HashPolicy) DeepCopyInto(out *HashPolicy) {
	*out = *in
	if in.CookieConfig != nil {
		in, out := &in.CookieConfig, &out.CookieConfig
		*out = new(CookieConfig)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HashPolicy.
func (in *HashPolicy) DeepCopy() *HashPolicy {
	if in == nil {
		return nil
	}
	out := new(HashPolicy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LeastRequestConfig) DeepCopyInto(out *LeastRequestConfig) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LeastRequestConfig.
func (in *LeastRequestConfig) DeepCopy() *LeastRequestConfig {
	if in == nil {
		return nil
	}
	out := new(LeastRequestConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LoadBalancer) DeepCopyInto(out *LoadBalancer) {
	*out = *in
	if in.RingHashConfig != nil {
		in, out := &in.RingHashConfig, &out.RingHashConfig
		*out = new(RingHashConfig)
		**out = **in
	}
	if in.LeastRequestConfig != nil {
		in, out := &in.LeastRequestConfig, &out.LeastRequestConfig
		*out = new(LeastRequestConfig)
		**out = **in
	}
	if in.HashPolicies != nil {
		in, out := &in.HashPolicies, &out.HashPolicies
		*out = make([]HashPolicy, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LoadBalancer.
func (in *LoadBalancer) DeepCopy() *LoadBalancer {
	if in == nil {
		return nil
	}
	out := new(LoadBalancer)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MeshGatewayConfig) DeepCopyInto(out *MeshGatewayConfig) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MeshGatewayConfig.
func (in *MeshGatewayConfig) DeepCopy() *MeshGatewayConfig {
	if in == nil {
		return nil
	}
	out := new(MeshGatewayConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ProxyDefaults) DeepCopyInto(out *ProxyDefaults) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ProxyDefaults.
func (in *ProxyDefaults) DeepCopy() *ProxyDefaults {
	if in == nil {
		return nil
	}
	out := new(ProxyDefaults)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ProxyDefaults) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ProxyDefaultsList) DeepCopyInto(out *ProxyDefaultsList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ProxyDefaults, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ProxyDefaultsList.
func (in *ProxyDefaultsList) DeepCopy() *ProxyDefaultsList {
	if in == nil {
		return nil
	}
	out := new(ProxyDefaultsList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ProxyDefaultsList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ProxyDefaultsSpec) DeepCopyInto(out *ProxyDefaultsSpec) {
	*out = *in
	if in.Config != nil {
		in, out := &in.Config, &out.Config
		*out = make(json.RawMessage, len(*in))
		copy(*out, *in)
	}
	out.MeshGateway = in.MeshGateway
	in.Expose.DeepCopyInto(&out.Expose)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ProxyDefaultsSpec.
func (in *ProxyDefaultsSpec) DeepCopy() *ProxyDefaultsSpec {
	if in == nil {
		return nil
	}
	out := new(ProxyDefaultsSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RingHashConfig) DeepCopyInto(out *RingHashConfig) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RingHashConfig.
func (in *RingHashConfig) DeepCopy() *RingHashConfig {
	if in == nil {
		return nil
	}
	out := new(RingHashConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceDefaults) DeepCopyInto(out *ServiceDefaults) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceDefaults.
func (in *ServiceDefaults) DeepCopy() *ServiceDefaults {
	if in == nil {
		return nil
	}
	out := new(ServiceDefaults)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ServiceDefaults) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceDefaultsList) DeepCopyInto(out *ServiceDefaultsList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ServiceDefaults, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceDefaultsList.
func (in *ServiceDefaultsList) DeepCopy() *ServiceDefaultsList {
	if in == nil {
		return nil
	}
	out := new(ServiceDefaultsList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ServiceDefaultsList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceDefaultsSpec) DeepCopyInto(out *ServiceDefaultsSpec) {
	*out = *in
	out.MeshGateway = in.MeshGateway
	in.Expose.DeepCopyInto(&out.Expose)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceDefaultsSpec.
func (in *ServiceDefaultsSpec) DeepCopy() *ServiceDefaultsSpec {
	if in == nil {
		return nil
	}
	out := new(ServiceDefaultsSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceIntentions) DeepCopyInto(out *ServiceIntentions) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceIntentions.
func (in *ServiceIntentions) DeepCopy() *ServiceIntentions {
	if in == nil {
		return nil
	}
	out := new(ServiceIntentions)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ServiceIntentions) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceIntentionsList) DeepCopyInto(out *ServiceIntentionsList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ServiceIntentions, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceIntentionsList.
func (in *ServiceIntentionsList) DeepCopy() *ServiceIntentionsList {
	if in == nil {
		return nil
	}
	out := new(ServiceIntentionsList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ServiceIntentionsList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceIntentionsSpec) DeepCopyInto(out *ServiceIntentionsSpec) {
	*out = *in
	out.Destination = in.Destination
	if in.Sources != nil {
		in, out := &in.Sources, &out.Sources
		*out = make(SourceIntentions, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(SourceIntention)
				**out = **in
			}
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceIntentionsSpec.
func (in *ServiceIntentionsSpec) DeepCopy() *ServiceIntentionsSpec {
	if in == nil {
		return nil
	}
	out := new(ServiceIntentionsSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceResolver) DeepCopyInto(out *ServiceResolver) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceResolver.
func (in *ServiceResolver) DeepCopy() *ServiceResolver {
	if in == nil {
		return nil
	}
	out := new(ServiceResolver)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ServiceResolver) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceResolverFailover) DeepCopyInto(out *ServiceResolverFailover) {
	*out = *in
	if in.Datacenters != nil {
		in, out := &in.Datacenters, &out.Datacenters
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceResolverFailover.
func (in *ServiceResolverFailover) DeepCopy() *ServiceResolverFailover {
	if in == nil {
		return nil
	}
	out := new(ServiceResolverFailover)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in ServiceResolverFailoverMap) DeepCopyInto(out *ServiceResolverFailoverMap) {
	{
		in := &in
		*out = make(ServiceResolverFailoverMap, len(*in))
		for key, val := range *in {
			(*out)[key] = *val.DeepCopy()
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceResolverFailoverMap.
func (in ServiceResolverFailoverMap) DeepCopy() ServiceResolverFailoverMap {
	if in == nil {
		return nil
	}
	out := new(ServiceResolverFailoverMap)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceResolverList) DeepCopyInto(out *ServiceResolverList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ServiceResolver, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceResolverList.
func (in *ServiceResolverList) DeepCopy() *ServiceResolverList {
	if in == nil {
		return nil
	}
	out := new(ServiceResolverList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ServiceResolverList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceResolverRedirect) DeepCopyInto(out *ServiceResolverRedirect) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceResolverRedirect.
func (in *ServiceResolverRedirect) DeepCopy() *ServiceResolverRedirect {
	if in == nil {
		return nil
	}
	out := new(ServiceResolverRedirect)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceResolverSpec) DeepCopyInto(out *ServiceResolverSpec) {
	*out = *in
	if in.Subsets != nil {
		in, out := &in.Subsets, &out.Subsets
		*out = make(ServiceResolverSubsetMap, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Redirect != nil {
		in, out := &in.Redirect, &out.Redirect
		*out = new(ServiceResolverRedirect)
		**out = **in
	}
	if in.Failover != nil {
		in, out := &in.Failover, &out.Failover
		*out = make(ServiceResolverFailoverMap, len(*in))
		for key, val := range *in {
			(*out)[key] = *val.DeepCopy()
		}
	}
	if in.LoadBalancer != nil {
		in, out := &in.LoadBalancer, &out.LoadBalancer
		*out = new(LoadBalancer)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceResolverSpec.
func (in *ServiceResolverSpec) DeepCopy() *ServiceResolverSpec {
	if in == nil {
		return nil
	}
	out := new(ServiceResolverSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceResolverSubset) DeepCopyInto(out *ServiceResolverSubset) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceResolverSubset.
func (in *ServiceResolverSubset) DeepCopy() *ServiceResolverSubset {
	if in == nil {
		return nil
	}
	out := new(ServiceResolverSubset)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in ServiceResolverSubsetMap) DeepCopyInto(out *ServiceResolverSubsetMap) {
	{
		in := &in
		*out = make(ServiceResolverSubsetMap, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceResolverSubsetMap.
func (in ServiceResolverSubsetMap) DeepCopy() ServiceResolverSubsetMap {
	if in == nil {
		return nil
	}
	out := new(ServiceResolverSubsetMap)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceRoute) DeepCopyInto(out *ServiceRoute) {
	*out = *in
	if in.Match != nil {
		in, out := &in.Match, &out.Match
		*out = new(ServiceRouteMatch)
		(*in).DeepCopyInto(*out)
	}
	if in.Destination != nil {
		in, out := &in.Destination, &out.Destination
		*out = new(ServiceRouteDestination)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceRoute.
func (in *ServiceRoute) DeepCopy() *ServiceRoute {
	if in == nil {
		return nil
	}
	out := new(ServiceRoute)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceRouteDestination) DeepCopyInto(out *ServiceRouteDestination) {
	*out = *in
	if in.RetryOnStatusCodes != nil {
		in, out := &in.RetryOnStatusCodes, &out.RetryOnStatusCodes
		*out = make([]uint32, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceRouteDestination.
func (in *ServiceRouteDestination) DeepCopy() *ServiceRouteDestination {
	if in == nil {
		return nil
	}
	out := new(ServiceRouteDestination)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceRouteHTTPMatch) DeepCopyInto(out *ServiceRouteHTTPMatch) {
	*out = *in
	if in.Header != nil {
		in, out := &in.Header, &out.Header
		*out = make([]ServiceRouteHTTPMatchHeader, len(*in))
		copy(*out, *in)
	}
	if in.QueryParam != nil {
		in, out := &in.QueryParam, &out.QueryParam
		*out = make([]ServiceRouteHTTPMatchQueryParam, len(*in))
		copy(*out, *in)
	}
	if in.Methods != nil {
		in, out := &in.Methods, &out.Methods
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceRouteHTTPMatch.
func (in *ServiceRouteHTTPMatch) DeepCopy() *ServiceRouteHTTPMatch {
	if in == nil {
		return nil
	}
	out := new(ServiceRouteHTTPMatch)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceRouteHTTPMatchHeader) DeepCopyInto(out *ServiceRouteHTTPMatchHeader) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceRouteHTTPMatchHeader.
func (in *ServiceRouteHTTPMatchHeader) DeepCopy() *ServiceRouteHTTPMatchHeader {
	if in == nil {
		return nil
	}
	out := new(ServiceRouteHTTPMatchHeader)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceRouteHTTPMatchQueryParam) DeepCopyInto(out *ServiceRouteHTTPMatchQueryParam) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceRouteHTTPMatchQueryParam.
func (in *ServiceRouteHTTPMatchQueryParam) DeepCopy() *ServiceRouteHTTPMatchQueryParam {
	if in == nil {
		return nil
	}
	out := new(ServiceRouteHTTPMatchQueryParam)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceRouteMatch) DeepCopyInto(out *ServiceRouteMatch) {
	*out = *in
	if in.HTTP != nil {
		in, out := &in.HTTP, &out.HTTP
		*out = new(ServiceRouteHTTPMatch)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceRouteMatch.
func (in *ServiceRouteMatch) DeepCopy() *ServiceRouteMatch {
	if in == nil {
		return nil
	}
	out := new(ServiceRouteMatch)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceRouter) DeepCopyInto(out *ServiceRouter) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceRouter.
func (in *ServiceRouter) DeepCopy() *ServiceRouter {
	if in == nil {
		return nil
	}
	out := new(ServiceRouter)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ServiceRouter) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceRouterList) DeepCopyInto(out *ServiceRouterList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ServiceRouter, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceRouterList.
func (in *ServiceRouterList) DeepCopy() *ServiceRouterList {
	if in == nil {
		return nil
	}
	out := new(ServiceRouterList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ServiceRouterList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceRouterSpec) DeepCopyInto(out *ServiceRouterSpec) {
	*out = *in
	if in.Routes != nil {
		in, out := &in.Routes, &out.Routes
		*out = make([]ServiceRoute, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceRouterSpec.
func (in *ServiceRouterSpec) DeepCopy() *ServiceRouterSpec {
	if in == nil {
		return nil
	}
	out := new(ServiceRouterSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceSplit) DeepCopyInto(out *ServiceSplit) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceSplit.
func (in *ServiceSplit) DeepCopy() *ServiceSplit {
	if in == nil {
		return nil
	}
	out := new(ServiceSplit)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in ServiceSplits) DeepCopyInto(out *ServiceSplits) {
	{
		in := &in
		*out = make(ServiceSplits, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceSplits.
func (in ServiceSplits) DeepCopy() ServiceSplits {
	if in == nil {
		return nil
	}
	out := new(ServiceSplits)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceSplitter) DeepCopyInto(out *ServiceSplitter) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceSplitter.
func (in *ServiceSplitter) DeepCopy() *ServiceSplitter {
	if in == nil {
		return nil
	}
	out := new(ServiceSplitter)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ServiceSplitter) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceSplitterList) DeepCopyInto(out *ServiceSplitterList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ServiceSplitter, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceSplitterList.
func (in *ServiceSplitterList) DeepCopy() *ServiceSplitterList {
	if in == nil {
		return nil
	}
	out := new(ServiceSplitterList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ServiceSplitterList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceSplitterSpec) DeepCopyInto(out *ServiceSplitterSpec) {
	*out = *in
	if in.Splits != nil {
		in, out := &in.Splits, &out.Splits
		*out = make(ServiceSplits, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceSplitterSpec.
func (in *ServiceSplitterSpec) DeepCopy() *ServiceSplitterSpec {
	if in == nil {
		return nil
	}
	out := new(ServiceSplitterSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SourceIntention) DeepCopyInto(out *SourceIntention) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SourceIntention.
func (in *SourceIntention) DeepCopy() *SourceIntention {
	if in == nil {
		return nil
	}
	out := new(SourceIntention)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in SourceIntentions) DeepCopyInto(out *SourceIntentions) {
	{
		in := &in
		*out = make(SourceIntentions, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(SourceIntention)
				**out = **in
			}
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SourceIntentions.
func (in SourceIntentions) DeepCopy() SourceIntentions {
	if in == nil {
		return nil
	}
	out := new(SourceIntentions)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Status) DeepCopyInto(out *Status) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make(Conditions, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Status.
func (in *Status) DeepCopy() *Status {
	if in == nil {
		return nil
	}
	out := new(Status)
	in.DeepCopyInto(out)
	return out
}
