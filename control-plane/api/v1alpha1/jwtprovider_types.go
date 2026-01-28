// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"encoding/base64"
	"encoding/json"
	"net/url"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul/api"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

const (
	JWTProviderKubeKind      string               = "jwtprovider"
	DiscoveryTypeStrictDNS   ClusterDiscoveryType = "STRICT_DNS"
	DiscoveryTypeStatic      ClusterDiscoveryType = "STATIC"
	DiscoveryTypeLogicalDNS  ClusterDiscoveryType = "LOGICAL_DNS"
	DiscoveryTypeEDS         ClusterDiscoveryType = "EDS"
	DiscoveryTypeOriginalDST ClusterDiscoveryType = "ORIGINAL_DST"
)

func init() {
	SchemeBuilder.Register(&JWTProvider{}, &JWTProviderList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// JWTProvider is the Schema for the jwtproviders API.
type JWTProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              JWTProviderSpec `json:"spec,omitempty"`
	Status            `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// JWTProviderList contains a list of JWTProvider.
type JWTProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []JWTProvider `json:"items"`
}

// JWTProviderSpec defines the desired state of JWTProvider
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type JWTProviderSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// JSONWebKeySet defines a JSON Web Key Set, its location on disk, or the
	// means with which to fetch a key set from a remote server.
	JSONWebKeySet *JSONWebKeySet `json:"jsonWebKeySet,omitempty"`

	// Issuer is the entity that must have issued the JWT.
	// This value must match the "iss" claim of the token.
	Issuer string `json:"issuer,omitempty"`

	// Audiences is the set of audiences the JWT is allowed to access.
	// If specified, all JWTs verified with this provider must address
	// at least one of these to be considered valid.
	Audiences []string `json:"audiences,omitempty"`

	// Locations where the JWT will be present in requests.
	// Envoy will check all of these locations to extract a JWT.
	// If no locations are specified Envoy will default to:
	// 1. Authorization header with Bearer schema:
	//    "Authorization: Bearer <token>"
	// 2. accessToken query parameter.
	Locations []*JWTLocation `json:"locations,omitempty"`

	// Forwarding defines rules for forwarding verified JWTs to the backend.
	Forwarding *JWTForwardingConfig `json:"forwarding,omitempty"`

	// ClockSkewSeconds specifies the maximum allowable time difference
	// from clock skew when validating the "exp" (Expiration) and "nbf"
	// (Not Before) claims.
	//
	// Default value is 30 seconds.
	ClockSkewSeconds int `json:"clockSkewSeconds,omitempty"`

	// CacheConfig defines configuration for caching the validation
	// result for previously seen JWTs. Caching results can speed up
	// verification when individual tokens are expected to be handled
	// multiple times.
	CacheConfig *JWTCacheConfig `json:"cacheConfig,omitempty"`
}

type JWTLocations []*JWTLocation

func (j JWTLocations) toConsul() []*capi.JWTLocation {
	if j == nil {
		return nil
	}
	result := make([]*capi.JWTLocation, 0, len(j))
	for _, loc := range j {
		result = append(result, loc.toConsul())
	}
	return result
}

func (j JWTLocations) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	for i, loc := range j {
		errs = append(errs, loc.validate(path.Index(i))...)
	}
	return errs
}

// JWTLocation is a location where the JWT could be present in requests.
//
// Only one of Header, QueryParam, or Cookie can be specified.
type JWTLocation struct {
	// Header defines how to extract a JWT from an HTTP request header.
	Header *JWTLocationHeader `json:"header,omitempty"`

	// QueryParam defines how to extract a JWT from an HTTP request
	// query parameter.
	QueryParam *JWTLocationQueryParam `json:"queryParam,omitempty"`

	// Cookie defines how to extract a JWT from an HTTP request cookie.
	Cookie *JWTLocationCookie `json:"cookie,omitempty"`
}

func (j *JWTLocation) toConsul() *capi.JWTLocation {
	if j == nil {
		return nil
	}
	return &capi.JWTLocation{
		Header:     j.Header.toConsul(),
		QueryParam: j.QueryParam.toConsul(),
		Cookie:     j.Cookie.toConsul(),
	}
}

func (j *JWTLocation) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if j == nil {
		return append(errs, field.Invalid(path, j, "location must not be nil"))
	}

	if 1 != countTrue(
		j.Header != nil,
		j.QueryParam != nil,
		j.Cookie != nil,
	) {
		asJSON, _ := json.Marshal(j)
		return append(errs, field.Invalid(path, string(asJSON), "exactly one of 'header', 'queryParam', or 'cookie' is required"))
	}

	errs = append(errs, j.Header.validate(path.Child("header"))...)
	errs = append(errs, j.QueryParam.validate(path.Child("queryParam"))...)
	errs = append(errs, j.Cookie.validate(path.Child("cookie"))...)
	return errs
}

// JWTLocationHeader defines how to extract a JWT from an HTTP
// request header.
type JWTLocationHeader struct {
	// Name is the name of the header containing the token.
	Name string `json:"name,omitempty"`

	// ValuePrefix is an optional prefix that precedes the token in the
	// header value.
	// For example, "Bearer " is a standard value prefix for a header named
	// "Authorization", but the prefix is not part of the token itself:
	// "Authorization: Bearer <token>"
	ValuePrefix string `json:"valuePrefix,omitempty"`

	// Forward defines whether the header with the JWT should be
	// forwarded after the token has been verified. If false, the
	// header will not be forwarded to the backend.
	//
	// Default value is false.
	Forward bool `json:"forward,omitempty"`
}

func (j *JWTLocationHeader) toConsul() *capi.JWTLocationHeader {
	if j == nil {
		return nil
	}
	return &capi.JWTLocationHeader{
		Name:        j.Name,
		ValuePrefix: j.ValuePrefix,
		Forward:     j.Forward,
	}
}

func (j *JWTLocationHeader) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if j == nil {
		return errs
	}

	if j.Name == "" {
		errs = append(errs, field.Invalid(path.Child("name"), j.Name, "JWT location header name is required"))
	}
	return errs
}

// JWTLocationQueryParam defines how to extract a JWT from an HTTP request query parameter.
type JWTLocationQueryParam struct {
	// Name is the name of the query param containing the token.
	Name string `json:"name,omitempty"`
}

func (j *JWTLocationQueryParam) toConsul() *capi.JWTLocationQueryParam {
	if j == nil {
		return nil
	}
	return &capi.JWTLocationQueryParam{
		Name: j.Name,
	}
}

func (j *JWTLocationQueryParam) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if j == nil {
		return nil
	}
	if j.Name == "" {
		errs = append(errs, field.Invalid(path.Child("name"), j.Name, "JWT location query parameter name is required"))
	}
	return errs
}

// JWTLocationCookie defines how to extract a JWT from an HTTP request cookie.
type JWTLocationCookie struct {
	// Name is the name of the cookie containing the token.
	Name string `json:"name,omitempty"`
}

func (j *JWTLocationCookie) toConsul() *capi.JWTLocationCookie {
	if j == nil {
		return nil
	}
	return &capi.JWTLocationCookie{
		Name: j.Name,
	}
}

func (j *JWTLocationCookie) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if j == nil {
		return nil
	}
	if j.Name == "" {
		errs = append(errs, field.Invalid(path.Child("name"), j.Name, "JWT location cookie name is required"))
	}
	return errs
}

type JWTForwardingConfig struct {
	// HeaderName is a header name to use when forwarding a verified
	// JWT to the backend. The verified JWT could have been extracted
	// from any location (query param, header, or cookie).
	//
	// The header value will be base64-URL-encoded, and will not be
	// padded unless PadForwardPayloadHeader is true.
	HeaderName string `json:"headerName,omitempty"`

	// PadForwardPayloadHeader determines whether padding should be added
	// to the base64 encoded token forwarded with ForwardPayloadHeader.
	//
	// Default value is false.
	PadForwardPayloadHeader bool `json:"padForwardPayloadHeader,omitempty"`
}

func (j *JWTForwardingConfig) toConsul() *capi.JWTForwardingConfig {
	if j == nil {
		return nil
	}
	return &capi.JWTForwardingConfig{
		HeaderName:              j.HeaderName,
		PadForwardPayloadHeader: j.PadForwardPayloadHeader,
	}
}

func (j *JWTForwardingConfig) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if j == nil {
		return nil
	}

	if j.HeaderName == "" {
		errs = append(errs, field.Invalid(path.Child("HeaderName"), j.HeaderName, "JWT forwarding header name is required"))
	}
	return errs
}

// JSONWebKeySet defines a key set, its location on disk, or the
// means with which to fetch a key set from a remote server.
//
// Exactly one of Local or Remote must be specified.
type JSONWebKeySet struct {
	// Local specifies a local source for the key set.
	Local *LocalJWKS `json:"local,omitempty"`

	// Remote specifies how to fetch a key set from a remote server.
	Remote *RemoteJWKS `json:"remote,omitempty"`
}

func (j *JSONWebKeySet) toConsul() *capi.JSONWebKeySet {
	if j == nil {
		return nil
	}

	return &capi.JSONWebKeySet{
		Local:  j.Local.toConsul(),
		Remote: j.Remote.toConsul(),
	}
}

func (j *JSONWebKeySet) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if j == nil {
		return append(errs, field.Invalid(path, j, "jsonWebKeySet is required"))
	}

	if countTrue(j.Local != nil, j.Remote != nil) != 1 {
		asJSON, _ := json.Marshal(j)
		return append(errs, field.Invalid(path, string(asJSON), "exactly one of 'local' or 'remote' is required"))
	}
	errs = append(errs, j.Local.validate(path.Child("local"))...)
	errs = append(errs, j.Remote.validate(path.Child("remote"))...)
	return errs
}

// LocalJWKS specifies a location for a local JWKS.
//
// Only one of String and Filename can be specified.
type LocalJWKS struct {
	// JWKS contains a base64 encoded JWKS.
	JWKS string `json:"jwks,omitempty"`

	// Filename configures a location on disk where the JWKS can be
	// found. If specified, the file must be present on the disk of ALL
	// proxies with intentions referencing this provider.
	Filename string `json:"filename,omitempty"`
}

func (l *LocalJWKS) toConsul() *capi.LocalJWKS {
	if l == nil {
		return nil
	}
	return &capi.LocalJWKS{
		JWKS:     l.JWKS,
		Filename: l.Filename,
	}
}

func (l *LocalJWKS) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if l == nil {
		return errs
	}

	if countTrue(l.JWKS != "", l.Filename != "") != 1 {
		asJSON, _ := json.Marshal(l)
		return append(errs, field.Invalid(path, string(asJSON), "Exactly one of 'jwks' or 'filename' is required"))
	}
	if l.JWKS != "" {
		if _, err := base64.StdEncoding.DecodeString(l.JWKS); err != nil {
			return append(errs, field.Invalid(path.Child("jwks"), l.JWKS, "JWKS must be a valid base64-encoded string"))
		}
	}
	return errs
}

// RemoteJWKS specifies how to fetch a JWKS from a remote server.
type RemoteJWKS struct {
	// URI is the URI of the server to query for the JWKS.
	URI string `json:"uri,omitempty"`

	// RequestTimeoutMs is the number of milliseconds to
	// time out when making a request for the JWKS.
	RequestTimeoutMs int `json:"requestTimeoutMs,omitempty"`

	// CacheDuration is the duration after which cached keys
	// should be expired.
	//
	// Default value is 5 minutes.
	CacheDuration metav1.Duration `json:"cacheDuration,omitempty"`

	// FetchAsynchronously indicates that the JWKS should be fetched
	// when a client request arrives. Client requests will be paused
	// until the JWKS is fetched.
	// If false, the proxy listener will wait for the JWKS to be
	// fetched before being activated.
	//
	// Default value is false.
	FetchAsynchronously bool `json:"fetchAsynchronously,omitempty"`

	// UseSNI determines whether the hostname should be set in SNI
	// header for TLS connection.
	//
	// Default value is false.
	UseSNI bool `json:",omitempty" alias:"use_sni"`

	// RetryPolicy defines a retry policy for fetching JWKS.
	//
	// There is no retry by default.
	RetryPolicy *JWKSRetryPolicy `json:"retryPolicy,omitempty"`

	// JWKSCluster defines how the specified Remote JWKS URI is to be fetched.
	JWKSCluster *JWKSCluster `json:"jwksCluster,omitempty"`
}

func (r *RemoteJWKS) toConsul() *capi.RemoteJWKS {
	if r == nil {
		return nil
	}
	return &capi.RemoteJWKS{
		URI:                 r.URI,
		RequestTimeoutMs:    r.RequestTimeoutMs,
		CacheDuration:       r.CacheDuration.Duration,
		FetchAsynchronously: r.FetchAsynchronously,
		UseSNI:              r.UseSNI,
		RetryPolicy:         r.RetryPolicy.toConsul(),
		JWKSCluster:         r.JWKSCluster.toConsul(),
	}
}

func (r *RemoteJWKS) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if r == nil {
		return errs
	}

	if r.URI == "" {
		errs = append(errs, field.Invalid(path.Child("uri"), r.URI, "remote JWKS URI is required"))
	} else if _, err := url.ParseRequestURI(r.URI); err != nil {
		errs = append(errs, field.Invalid(path.Child("uri"), r.URI, "remote JWKS URI is invalid"))
	}

	errs = append(errs, r.RetryPolicy.validate(path.Child("retryPolicy"))...)
	errs = append(errs, r.JWKSCluster.validate(path.Child("jwksCluster"))...)
	return errs
}

// JWKSCluster defines how the specified Remote JWKS URI is to be fetched.
type JWKSCluster struct {
	// DiscoveryType refers to the service discovery type to use for resolving the cluster.
	//
	// This defaults to STRICT_DNS.
	// Other options include STATIC, LOGICAL_DNS, EDS or ORIGINAL_DST.
	DiscoveryType ClusterDiscoveryType `json:"discoveryType,omitempty"`

	// TLSCertificates refers to the data containing certificate authority certificates to use
	// in verifying a presented peer certificate.
	// If not specified and a peer certificate is presented it will not be verified.
	//
	// Must be either CaCertificateProviderInstance or TrustedCA.
	TLSCertificates *JWKSTLSCertificate `json:"tlsCertificates,omitempty"`

	// The timeout for new network connections to hosts in the cluster.
	// If not set, a default value of 5s will be used.
	ConnectTimeout metav1.Duration `json:"connectTimeout,omitempty"`
}

func (c *JWKSCluster) toConsul() *capi.JWKSCluster {
	if c == nil {
		return nil
	}
	return &capi.JWKSCluster{
		DiscoveryType:   c.DiscoveryType.toConsul(),
		TLSCertificates: c.TLSCertificates.toConsul(),
		ConnectTimeout:  c.ConnectTimeout.Duration,
	}
}

func (c *JWKSCluster) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if c == nil {
		return errs
	}

	errs = append(errs, c.DiscoveryType.validate(path.Child("discoveryType"))...)
	errs = append(errs, c.TLSCertificates.validate(path.Child("tlsCertificates"))...)

	return errs
}

type ClusterDiscoveryType string

func (d ClusterDiscoveryType) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList

	switch d {
	case DiscoveryTypeStatic, DiscoveryTypeStrictDNS, DiscoveryTypeLogicalDNS, DiscoveryTypeEDS, DiscoveryTypeOriginalDST:
		return errs
	default:
		errs = append(errs, field.Invalid(path, string(d), "unsupported jwks cluster discovery type."))
	}
	return errs
}

func (d ClusterDiscoveryType) toConsul() capi.ClusterDiscoveryType {
	return capi.ClusterDiscoveryType(string(d))
}

// JWKSTLSCertificate refers to the data containing certificate authority certificates to use
// in verifying a presented peer certificate.
// If not specified and a peer certificate is presented it will not be verified.
//
// Must be either CaCertificateProviderInstance or TrustedCA.
type JWKSTLSCertificate struct {
	// CaCertificateProviderInstance Certificate provider instance for fetching TLS certificates.
	CaCertificateProviderInstance *JWKSTLSCertProviderInstance `json:"caCertificateProviderInstance,omitempty"`

	// TrustedCA defines TLS certificate data containing certificate authority certificates
	// to use in verifying a presented peer certificate.
	//
	// Exactly one of Filename, EnvironmentVariable, InlineString or InlineBytes must be specified.
	TrustedCA *JWKSTLSCertTrustedCA `json:"trustedCA,omitempty"`
}

func (c *JWKSTLSCertificate) toConsul() *capi.JWKSTLSCertificate {
	if c == nil {
		return nil
	}

	return &capi.JWKSTLSCertificate{
		TrustedCA:                     c.TrustedCA.toConsul(),
		CaCertificateProviderInstance: c.CaCertificateProviderInstance.toConsul(),
	}
}

func (c *JWKSTLSCertificate) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if c == nil {
		return errs
	}

	hasProviderInstance := c.CaCertificateProviderInstance != nil
	hasTrustedCA := c.TrustedCA != nil

	if countTrue(hasTrustedCA, hasProviderInstance) != 1 {
		asJSON, _ := json.Marshal(c)
		errs = append(errs, field.Invalid(path, string(asJSON), "exactly one of 'trustedCa' or 'caCertificateProviderInstance' is required"))
	}

	errs = append(errs, c.TrustedCA.validate(path.Child("trustedCa"))...)

	return errs
}

// JWKSTLSCertProviderInstance Certificate provider instance for fetching TLS certificates.
type JWKSTLSCertProviderInstance struct {
	// InstanceName refers to the certificate provider instance name.
	//
	// The default value is "default".
	InstanceName string `json:"instanceName,omitempty"`

	// CertificateName is used to specify certificate instances or types. For example, "ROOTCA" to specify
	// a root-certificate (validation context) or "example.com" to specify a certificate for a
	// particular domain.
	//
	// The default value is the empty string.
	CertificateName string `json:"certificateName,omitempty"`
}

func (c *JWKSTLSCertProviderInstance) toConsul() *capi.JWKSTLSCertProviderInstance {
	if c == nil {
		return nil
	}

	return &capi.JWKSTLSCertProviderInstance{
		InstanceName:    c.InstanceName,
		CertificateName: c.CertificateName,
	}
}

// JWKSTLSCertTrustedCA defines TLS certificate data containing certificate authority certificates
// to use in verifying a presented peer certificate.
//
// Exactly one of Filename, EnvironmentVariable, InlineString or InlineBytes must be specified.
type JWKSTLSCertTrustedCA struct {
	Filename            string `json:"filename,omitempty"`
	EnvironmentVariable string `json:"environmentVariable,omitempty"`
	InlineString        string `json:"inlineString,omitempty"`
	InlineBytes         []byte `json:"inlineBytes,omitempty"`
}

func (c *JWKSTLSCertTrustedCA) toConsul() *capi.JWKSTLSCertTrustedCA {
	if c == nil {
		return nil
	}

	return &capi.JWKSTLSCertTrustedCA{
		Filename:            c.Filename,
		EnvironmentVariable: c.EnvironmentVariable,
		InlineBytes:         c.InlineBytes,
		InlineString:        c.InlineString,
	}
}

func (c *JWKSTLSCertTrustedCA) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if c == nil {
		return errs
	}

	hasFilename := c.Filename != ""
	hasEnv := c.EnvironmentVariable != ""
	hasInlineBytes := len(c.InlineBytes) > 0
	hasInlineString := c.InlineString != ""

	if countTrue(hasFilename, hasEnv, hasInlineString, hasInlineBytes) != 1 {
		asJSON, _ := json.Marshal(c)
		errs = append(errs, field.Invalid(path, string(asJSON), "exactly one of 'filename', 'environmentVariable', 'inlineString' or 'inlineBytes' is required"))
	}
	return errs
}

// JWKSRetryPolicy defines a retry policy for fetching JWKS.
//
// There is no retry by default.
type JWKSRetryPolicy struct {
	// NumRetries is the number of times to retry fetching the JWKS.
	// The retry strategy uses jittered exponential backoff with
	// a base interval of 1s and max of 10s.
	//
	// Default value is 0.
	NumRetries int `json:"numRetries,omitempty"`

	// Retry's backoff policy.
	//
	// Defaults to Envoy's backoff policy.
	RetryPolicyBackOff *RetryPolicyBackOff `json:"retryPolicyBackOff,omitempty"`
}

func (j *JWKSRetryPolicy) toConsul() *capi.JWKSRetryPolicy {
	if j == nil {
		return nil
	}
	return &capi.JWKSRetryPolicy{
		NumRetries:         j.NumRetries,
		RetryPolicyBackOff: j.RetryPolicyBackOff.toConsul(),
	}
}

func (j *JWKSRetryPolicy) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if j == nil {
		return errs
	}

	return append(errs, j.RetryPolicyBackOff.validate(path.Child("retryPolicyBackOff"))...)
}

// RetryPolicyBackOff defines retry's policy backoff.
//
// Defaults to Envoy's backoff policy.
type RetryPolicyBackOff struct {
	// BaseInterval to be used for the next back off computation.
	//
	// The default value from envoy is 1s.
	BaseInterval metav1.Duration `json:"baseInterval,omitempty"`

	// MaxInternal to be used to specify the maximum interval between retries.
	// Optional but should be greater or equal to BaseInterval.
	//
	// Defaults to 10 times BaseInterval.
	MaxInterval metav1.Duration `json:"maxInterval,omitempty"`
}

func (r *RetryPolicyBackOff) toConsul() *capi.RetryPolicyBackOff {
	if r == nil {
		return nil
	}
	return &capi.RetryPolicyBackOff{
		BaseInterval: r.BaseInterval.Duration,
		MaxInterval:  r.MaxInterval.Duration,
	}
}

func (r *RetryPolicyBackOff) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if r == nil {
		return errs
	}

	if (r.MaxInterval.Duration != 0) && (r.BaseInterval.Duration > r.MaxInterval.Duration) {
		asJSON, _ := json.Marshal(r)
		errs = append(errs, field.Invalid(path, string(asJSON), "maxInterval should be greater or equal to baseInterval"))
	}
	return errs
}

type JWTCacheConfig struct {
	// Size specifies the maximum number of JWT verification
	// results to cache.
	//
	// Defaults to 0, meaning that JWT caching is disabled.
	Size int `json:"size,omitempty"`
}

func (j *JWTCacheConfig) toConsul() *capi.JWTCacheConfig {
	if j == nil {
		return nil
	}
	return &capi.JWTCacheConfig{
		Size: j.Size,
	}
}

func (j *JWTProvider) GetObjectMeta() metav1.ObjectMeta {
	return j.ObjectMeta
}

func (j *JWTProvider) AddFinalizer(name string) {
	j.ObjectMeta.Finalizers = append(j.Finalizers(), name)
}

func (j *JWTProvider) RemoveFinalizer(name string) {
	var newFinalizers []string
	for _, oldF := range j.Finalizers() {
		if oldF != name {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	j.ObjectMeta.Finalizers = newFinalizers
}

func (j *JWTProvider) Finalizers() []string {
	return j.ObjectMeta.Finalizers
}

func (j *JWTProvider) ConsulKind() string {
	return capi.JWTProvider
}

func (j *JWTProvider) ConsulGlobalResource() bool {
	return true
}

func (j *JWTProvider) ConsulMirroringNS() string {
	return common.DefaultConsulNamespace
}

func (j *JWTProvider) KubeKind() string {
	return JWTProviderKubeKind
}

func (j *JWTProvider) ConsulName() string {
	return j.ObjectMeta.Name
}

func (j *JWTProvider) KubernetesName() string {
	return j.ObjectMeta.Name
}

func (j *JWTProvider) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
	j.Status.Conditions = Conditions{
		{
			Type:               ConditionSynced,
			Status:             status,
			LastTransitionTime: metav1.Now(),
			Reason:             reason,
			Message:            message,
		},
	}
}

func (j *JWTProvider) SetLastSyncedTime(time *metav1.Time) {
	j.Status.LastSyncedTime = time
}

// SyncedCondition gets the synced condition.
func (j *JWTProvider) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := j.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

// SyncedConditionStatus returns the status of the synced condition.
func (j *JWTProvider) SyncedConditionStatus() corev1.ConditionStatus {
	cond := j.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown
	}
	return cond.Status
}

// ToConsul converts the resource to the corresponding Consul API definition.
// Its return type is the generic ConfigEntry but a specific config entry
// type should be constructed e.g. ServiceConfigEntry.
func (j *JWTProvider) ToConsul(datacenter string) api.ConfigEntry {
	return &capi.JWTProviderConfigEntry{
		Kind:             j.ConsulKind(),
		Name:             j.ConsulName(),
		JSONWebKeySet:    j.Spec.JSONWebKeySet.toConsul(),
		Issuer:           j.Spec.Issuer,
		Audiences:        j.Spec.Audiences,
		Locations:        JWTLocations(j.Spec.Locations).toConsul(),
		Forwarding:       j.Spec.Forwarding.toConsul(),
		ClockSkewSeconds: j.Spec.ClockSkewSeconds,
		CacheConfig:      j.Spec.CacheConfig.toConsul(),
		Meta:             meta(datacenter),
	}
}

// MatchesConsul returns true if the resource has the same fields as the Consul
// config entry.
func (j *JWTProvider) MatchesConsul(candidate api.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.JWTProviderConfigEntry)
	if !ok {
		return false
	}
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(j.ToConsul(""), configEntry, cmpopts.IgnoreFields(capi.JWTProviderConfigEntry{}, "Partition", "Namespace", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty())
}

// Validate returns an error if the resource is invalid.
func (j *JWTProvider) Validate(consulMeta common.ConsulMeta) error {
	var errs field.ErrorList
	path := field.NewPath("spec")

	errs = append(errs, j.Spec.JSONWebKeySet.validate(path.Child("jsonWebKeySet"))...)
	errs = append(errs, JWTLocations(j.Spec.Locations).validate(path.Child("locations"))...)
	errs = append(errs, j.Spec.Forwarding.validate(path.Child("forwarding"))...)
	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: JWTProviderKubeKind},
			j.KubernetesName(), errs)
	}
	return nil
}

// DefaultNamespaceFields sets Consul namespace fields on the config entry
// spec to their default values if namespaces are enabled.
func (j *JWTProvider) DefaultNamespaceFields(_ common.ConsulMeta) {}

func countTrue(vals ...bool) int {
	var result int
	for _, v := range vals {
		if v {
			result++
		}
	}
	return result
}

var _ common.ConfigEntryResource = (*JWTProvider)(nil)
