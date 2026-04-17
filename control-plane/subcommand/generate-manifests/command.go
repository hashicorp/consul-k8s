// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package generatemanifests

// Currently we are generating manifests.
// 1. We generate all the manifests for the gateway API obkects under API version gateway.networking.k8s.io, update the version from v1beta1 to v1
// for gateways, httproutes; we retain v1beta1 to referencegrants and v1alpha2 tcproutes and dump them into the output dir.
// 2. We also generate manifests for consul.hashicorp.com API group when the user opts for it.

// NOTE:There is a high chance that all the manifests generated might not have the helm release name, because the applications httproutes can be a
// separate helm release and the gateway can be a separate helm release.
// OR the httproutes or any obj is a direct install with kubectl and not managed by helm at all.

// TODO: Thus the way to identify the relevant manifests it to look for the parentref of the obj, then get the gatewayclass of the gateway referred
// then get the helm release name from the gatewayclass labels, and only dump the manifests for the objects which have the same helm release name as the flag provided by user.
// labels to look for: release: <release-name-flag>

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mitchellh/cli"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

const (
	kindGateway                  = "Gateway"
	kindHTTPRoute                = "HTTPRoute"
	kindGRPCRoute                = "GRPCRoute"
	kindReferenceGrant           = "ReferenceGrant"
	kindTCPRoute                 = "TCPRoute"
	kindTLSRoute                 = "TLSRoute"
	kindUDPRoute                 = "UDPRoute"
	consulAPIGroup               = "consul.hashicorp.com"
	consulAPIVersionV1Beta1      = "v1beta1"
	consulAPIVersionV1Alpha2     = "v1alpha2"
	K8sGatewayAPIGroup           = "gateway.networking.k8s.io"
	K8sGatewayAPIVersionV1       = "v1"
	K8sGatewayAPIVersionV1Beta1  = "v1beta1"
	K8sGatewayAPIVersionV1Alpha2 = "v1alpha2"
	GatewayClassControllerName   = "consul.hashicorp.com/gateway-controller"
)

type Command struct {
	UI cli.Ui

	flags *flag.FlagSet
	k8s   *flags.K8SFlags

	flagChart                  string
	flagApp                    string
	flagRelease                string
	flagManifestsGatewayAPIDir string
	flagManifestsConsulAPIDir  string

	flagOpenshiftSCCName string

	k8sClient client.Client

	once sync.Once
	help string

	consulApiEnabled bool

	ctx context.Context

	// // For test injection
	// AddToSchemeFunc func(*runtime.Scheme) error
}

var gatewayClassMap = map[string]bool{}
var gatewayMap = map[string]bool{}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)

	c.flags.StringVar(&c.flagChart, "chart", "",
		"Helm chart name for created objects.")
	c.flags.StringVar(&c.flagApp, "app", "",
		"Helm chart app for created objects.")
	c.flags.StringVar(&c.flagRelease, "release-name", "",
		"Helm chart release for created objects.")

	c.flags.StringVar(&c.flagOpenshiftSCCName, "openshift-scc-name", "",
		"Name of security context constraint to use for gateways on Openshift.",
	)
	//for consul.hashicorp.com API group
	c.flags.BoolVar(&c.consulApiEnabled, "consulapi-enabled", false,
		"Whether to generate manifests for gateway resources under consul.hashicorp.com API group.")

	c.flags.StringVar(
		&c.flagManifestsGatewayAPIDir,
		"manifests-gatewayapi-dir",
		"/output/gatewayapi",
		"Directory where Gateway API objects will be dumped.",
	)

	c.flags.StringVar(
		&c.flagManifestsConsulAPIDir,
		"manifests-consulapi-dir",
		"/output/consulapi",
		"Directory where Consul API objects will be dumped. This is only applicable if -consulapi-enabled is set to true.",
	)

	c.k8s = &flags.K8SFlags{}
	flags.Merge(c.flags, c.k8s.Flags())
	c.help = flags.Usage(help, c.flags)
}

func (c *Command) Run(args []string) int {
	var err error
	c.once.Do(c.init)
	if err = c.flags.Parse(args); err != nil {
		return 1
	}
	// Validate flags
	if err := c.validateFlags(); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	if c.ctx == nil {
		c.ctx = context.Background()
	}

	// Create the Kubernetes client
	if c.k8sClient == nil {
		config, err := subcommand.K8SConfig(c.k8s.KubeConfig())
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error retrieving Kubernetes auth: %s", err))
			return 1
		}

		s := runtime.NewScheme()
		if err := clientgoscheme.AddToScheme(s); err != nil {
			c.UI.Error(fmt.Sprintf("Could not add client-go schema: %s", err))
			return 1
		}
		// apiextensions schema is needed to delete crds
		if err := apiextensions.AddToScheme(s); err != nil {
			c.UI.Error(fmt.Sprintf("Could not add apiextensions schema: %s", err))
			return 1
		}
		if err := gwv1beta1.Install(s); err != nil {
			c.UI.Error(fmt.Sprintf("Could not add api-gateway schema: %s", err))
			return 1
		}
		if err := gwv1alpha2.Install(s); err != nil {
			c.UI.Error(fmt.Sprintf("Could not add api-gateway v1alpha2 schema: %s", err))
			return 1
		}
		if err := gwv1.Install(s); err != nil {
			c.UI.Error(fmt.Sprintf("Could not add api-gateway v1 schema: %s", err))
			return 1
		}
		if err := v1alpha1.AddToScheme(s); err != nil {
			c.UI.Error(fmt.Sprintf("Could not add consul-k8s schema: %s", err))
			return 1
		}

		c.k8sClient, err = client.New(config, client.Options{Scheme: s})
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error initializing Kubernetes client: %s", err))
			return 1
		}
	}

	// update the gatewayclass map
	c.buildMaps()

	fmt.Print("Updated the gwclass map\n")
	fmt.Printf("Update gatewayclass map %+v\n", gatewayClassMap)
	fmt.Printf("Update gateway map %+v\n", gatewayMap)
	// Fetch all the resources using the gateway.networking.k8s.io API in the cluster
	if err := c.dumpGatewayAPIObjects(); err != nil {
		c.UI.Error(fmt.Sprintf("Error dumping Gateway API objects: %s", err))
		return 1
	}
	time.Sleep(20 * time.Second)

	c.UI.Info(fmt.Sprintf("✅ Gateway API objects dumped into: %s", c.flagManifestsGatewayAPIDir))
	return 0

}

/*

Lets do as below:
Lets get the gatewayclasses in the cluster and create a map of gatewayclass name to controller name, then
get the gateways in the cluster, then for each gateway get the gatewayclass,
then check the controller name in the gatewayclass if it is controlled by consul.
Lets create a map of gatewayname and bool for whether it is controlled by consul or not.

Then we get the other resources like httproutes, grpcroutes, referencegrants, tcproutes, tlsroutes, udproutes
and check their parentref for the gateway, then check if the gateway is in the map and if it is controlled by consul or not.
 If it is controlled by consul then we en force the consulApiVersion and agatewayApiVersion and dump the manifests.
 If not controlled by consul we only enforce the gatewayApiVersion and dump the manifests.


*/

func (c *Command) dumpGatewayAPIObjects() error {
	if c.k8sClient == nil {
		return fmt.Errorf("k8s client is nil")
	}

	// Ensure base output dir exists
	if err := os.MkdirAll(c.flagManifestsGatewayAPIDir, 0755); err != nil {
		return err
	}
	c.UI.Info(fmt.Sprintf("Dumping Gateway API objects... in %s", c.flagManifestsGatewayAPIDir))

	ctx := c.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// Dump resources no gc is needed to set up
	fmt.Printf("fetching gwclass\n")
	fmt.Printf("TYPE: %T\n", &gwv1.GatewayClassList{})
	// if err := c.dumpTypedList(ctx, "gatewayclasses", &gwv1.GatewayClassList{}); err != nil {
	// 	fmt.Printf("Error dumping gatewayclasses from gateway.networking.k8s.io/v1: %v\n", err)
	// 	if err := c.dumpTypedList(ctx, "gatewayclasses", &gwv1beta1.GatewayClassList{}); err != nil {
	// 		fmt.Printf("Error dumping gatewayclasses from gateway.networking.k8s.io/v1beta1: %v\n", err)
	// 		c.UI.Info(fmt.Sprintf("Skipping GatewayClass dump: %v", err))
	// 	}
	// }
	if err := c.dumpTypedList(ctx, "gateways", &gwv1.GatewayList{}); err != nil {
		if err := c.dumpTypedList(ctx, "gateways", &gwv1beta1.GatewayList{}); err != nil {
			c.UI.Info(fmt.Sprintf("Skipping Gateway dump: %v", err))
		}
	}
	if err := c.dumpTypedList(ctx, "httproutes", &gwv1.HTTPRouteList{}); err != nil {
		if err := c.dumpTypedList(ctx, "httproutes", &gwv1beta1.HTTPRouteList{}); err != nil {
			c.UI.Info(fmt.Sprintf("Skipping HTTPRoute dump: %v", err))
		}
	}
	if err := c.dumpTypedList(ctx, "grpcroutes", &gwv1.GRPCRouteList{}); err != nil {
		if err := c.dumpTypedList(ctx, "grpcroutes", &gwv1alpha2.GRPCRouteList{}); err != nil {
			c.UI.Info(fmt.Sprintf("Skipping GRPCRoute dump: %v", err))
		}
	}

	// fetch referenceGrants from gwv1beta1
	if err := c.dumpTypedList(ctx, "referencegrants", &gwv1beta1.ReferenceGrantList{}); err != nil {
		c.UI.Info(fmt.Sprintf("Skipping ReferenceGrant dump: %v", err))
	}

	// fetch tcproutes from gwv1alha2
	if err := c.dumpTypedList(ctx, "tcproutes", &gwv1alpha2.TCPRouteList{}); err != nil {
		c.UI.Info(fmt.Sprintf("Skipping TCPRoute dump: %v", err))
	}

	// fetch tlsroutes from gwv1alpha2
	if err := c.dumpTypedList(ctx, "tlsroutes", &gwv1alpha2.TLSRouteList{}); err != nil {
		c.UI.Info(fmt.Sprintf("Skipping TLSRoute dump: %v", err))
	}

	// fetch udproutes from gwv1alpha2
	if err := c.dumpTypedList(ctx, "udproutes", &gwv1alpha2.UDPRouteList{}); err != nil {
		c.UI.Info(fmt.Sprintf("Skipping UDPRoute dump: %v", err))
	}

	return nil
}

func (c *Command) dumpTypedList(ctx context.Context, kindDir string, list client.ObjectList) error {
	if err := c.k8sClient.List(ctx, list); err != nil {
		return err
	}

	// Convert list -> []client.Object
	items, err := extractItems(list)
	if err != nil {
		return err
	}

	return c.writeObjects(kindDir, items)

}

func gwKey(ns, name string) string {
	return ns + "/" + name
}

func getKind(raw map[string]interface{}) string {
	k, _ := raw["kind"].(string)
	return k
}

func getMetadata(raw map[string]interface{}) map[string]interface{} {
	m, _ := raw["metadata"].(map[string]interface{})
	return m
}

func getSpec(raw map[string]interface{}) map[string]interface{} {
	s, ok := raw["spec"].(map[string]interface{})
	if !ok {
		return nil
	}
	return s
}

func (c *Command) buildMaps() {
	// gateway class map
	gwcV1 := gwv1.GatewayClassList{}
	if err := c.k8sClient.List(c.ctx, &gwcV1); err == nil {
		for _, gwc := range gwcV1.Items {
			gatewayClassMap[gwc.Name] = gwc.Spec.ControllerName == GatewayClassControllerName
		}
	} else {
		gwcV1Beta1 := gwv1beta1.GatewayClassList{}
		if err := c.k8sClient.List(c.ctx, &gwcV1Beta1); err == nil {
			for _, gwc := range gwcV1Beta1.Items {
				gatewayClassMap[gwc.Name] = gwc.Spec.ControllerName == GatewayClassControllerName
			}
		}
	}

	// gateway map
	gwV1 := gwv1.GatewayList{}
	if err := c.k8sClient.List(c.ctx, &gwV1); err == nil {
		for _, gw := range gwV1.Items {
			key := gwKey(gw.Namespace, gw.Name)
			class := string(gw.Spec.GatewayClassName)
			gatewayMap[key] = gatewayClassMap[class]
		}
	} else {
		gwV1Beta1 := gwv1beta1.GatewayList{}
		if err := c.k8sClient.List(c.ctx, &gwV1Beta1); err == nil {
			for _, gw := range gwV1Beta1.Items {
				key := gwKey(gw.Namespace, gw.Name)
				class := string(gw.Spec.GatewayClassName)
				gatewayMap[key] = gatewayClassMap[class]
			}

		}
	}

}

func enforceGatewayAPIVersion(raw map[string]interface{}) {
	kind, _ := raw["kind"].(string)
	if kind == "" {
		return
	}

	switch kind {
	// by default
	// for gateway.networking.k8s.io/v1
	case kindGateway:
		raw["apiVersion"] = K8sGatewayAPIGroup + "/" + K8sGatewayAPIVersionV1

	case kindHTTPRoute, kindGRPCRoute:
		raw["apiVersion"] = K8sGatewayAPIGroup + "/" + K8sGatewayAPIVersionV1

	// ReferenceGrant -> v1beta1
	case kindReferenceGrant:
		raw["apiVersion"] = K8sGatewayAPIGroup + "/" + K8sGatewayAPIVersionV1Beta1

	// UDP/TLS/TCP -> v1alpha2
	case kindUDPRoute, kindTLSRoute, kindTCPRoute:
		raw["apiVersion"] = K8sGatewayAPIGroup + "/" + K8sGatewayAPIVersionV1Alpha2
	}
}

func convertToConsulGateway(raw map[string]interface{}) {
	raw["apiVersion"] = consulAPIGroup + "/" + consulAPIVersionV1Beta1
	spec := getSpec(raw)
	meta := getMetadata(raw)
	if spec == nil {
		return
	}
	spec["gatewayClassName"] = "consul-custom"
	meta["name"] = meta["name"].(string) + "-custom"

	// update listeners
	listeners, ok := spec["listeners"].([]interface{})
	if !ok {
		return
	}
	for _, listener := range listeners {
		listenerMap, ok := listener.(map[string]interface{})
		if !ok {
			continue
		}
		allowedRoutes, ok := listenerMap["allowedRoutes"].(map[string]interface{})
		if !ok {
			continue
		}
		kinds, ok := allowedRoutes["kinds"].([]interface{})
		if !ok {
			continue
		}
		for _, kind := range kinds {
			kindMap, ok := kind.(map[string]interface{})
			if !ok {
				continue
			}
			if kindMap["group"] == K8sGatewayAPIGroup {
				kindMap["group"] = consulAPIGroup
			}
		}

	}

}

func hasConsulParent(raw map[string]interface{}) bool {
	spec := getSpec(raw)
	if spec == nil {
		return false
	}
	parentRefs, ok := spec["parentRefs"].([]interface{})
	if !ok {
		return false
	}
	found := false
	for _, pr := range parentRefs {
		prMap, ok := pr.(map[string]interface{})
		if !ok {
			continue
		}
		if prMap["kind"] == "Gateway" {
			gwName := prMap["name"].(string)
			gwNamespace, ok := prMap["namespace"].(string)
			if !ok || gwNamespace == "" {
				gwNamespace = getMetadata(raw)["namespace"].(string)
			}
			if gatewayMap[gwKey(gwNamespace, gwName)] {
				prMap["name"] = prMap["name"].(string) + "-custom"
				prMap["group"] = consulAPIGroup
				found = true
			}
		}
	}
	return found
}

func convertToConsulRoute(raw map[string]interface{}) {
	kind := getKind(raw)
	meta := getMetadata(raw)

	if kind == kindHTTPRoute {
		raw["apiVersion"] = consulAPIGroup + "/" + consulAPIVersionV1Beta1
	} else {
		raw["apiVersion"] = consulAPIGroup + "/" + consulAPIVersionV1Alpha2
	}

	meta["name"] = meta["name"].(string) + "-custom"
}

func convertReferenceGrant(raw map[string]interface{}) {
	meta := getMetadata(raw)
	spec := getSpec(raw)

	from, ok := spec["from"].([]interface{})
	if !ok {
		return
	}

	for _, f := range from {
		fm := f.(map[string]interface{})

		if fm["kind"] == "Gateway" {
			name := fm["name"].(string)
			ns, ok := fm["namespace"].(string)
			if !ok || ns == "" {
				ns = meta["namespace"].(string)
			}

			if gatewayMap[gwKey(ns, name)] {
				fm["name"] = name + "-custom"
				fm["group"] = consulAPIGroup

			}
		} else if fm["group"] == K8sGatewayAPIGroup {
			fm["group"] = consulAPIGroup
		}
	}

	raw["apiVersion"] = consulAPIGroup + "/" + consulAPIVersionV1Beta1
	meta["name"] = meta["name"].(string) + "-custom"

}

func enforceConsulApiVersion(raw map[string]interface{}) bool {
	fmt.Printf("gatewayMap %+v", gatewayMap)
	kind := getKind(raw)
	metadata := getMetadata(raw)
	if kind == "" {
		return false
	}

	switch kind {

	case kindGateway:
		key := gwKey(metadata["namespace"].(string), metadata["name"].(string))
		if !gatewayMap[key] {
			return false
		}
		convertToConsulGateway(raw)
		return true

	case kindHTTPRoute, kindGRPCRoute, kindUDPRoute, kindTLSRoute, kindTCPRoute:
		if !hasConsulParent(raw) {
			return false
		}
		convertToConsulRoute(raw)

	case kindReferenceGrant:
		convertReferenceGrant(raw)

	}
	return true
}

func extractItems(list client.ObjectList) ([]client.Object, error) {
	switch v := list.(type) {

	case *gwv1.GatewayClassList:
		out := make([]client.Object, 0, len(v.Items))
		for i := range v.Items {
			out = append(out, &v.Items[i])
		}
		return out, nil

	case *gwv1beta1.GatewayClassList:
		out := make([]client.Object, 0, len(v.Items))
		for i := range v.Items {
			out = append(out, &v.Items[i])
		}
		return out, nil

	case *gwv1.GatewayList:
		out := make([]client.Object, 0, len(v.Items))
		for i := range v.Items {
			out = append(out, &v.Items[i])
		}
		return out, nil

	case *gwv1beta1.GatewayList:
		out := make([]client.Object, 0, len(v.Items))
		for i := range v.Items {
			out = append(out, &v.Items[i])
		}
		return out, nil

	case *gwv1.HTTPRouteList:
		out := make([]client.Object, 0, len(v.Items))
		for i := range v.Items {
			out = append(out, &v.Items[i])
		}
		return out, nil

	case *gwv1beta1.HTTPRouteList:
		out := make([]client.Object, 0, len(v.Items))
		for i := range v.Items {
			out = append(out, &v.Items[i])
		}
		return out, nil

	case *gwv1.GRPCRouteList:
		out := make([]client.Object, 0, len(v.Items))
		for i := range v.Items {
			out = append(out, &v.Items[i])
		}
		return out, nil

	case *gwv1alpha2.GRPCRouteList:
		out := make([]client.Object, 0, len(v.Items))
		for i := range v.Items {
			out = append(out, &v.Items[i])
		}
		return out, nil

	case *gwv1beta1.ReferenceGrantList:
		out := make([]client.Object, 0, len(v.Items))
		for i := range v.Items {
			out = append(out, &v.Items[i])
		}
		return out, nil

	case *gwv1alpha2.TCPRouteList:
		out := make([]client.Object, 0, len(v.Items))
		for i := range v.Items {
			out = append(out, &v.Items[i])
		}
		return out, nil

	case *gwv1alpha2.TLSRouteList:
		out := make([]client.Object, 0, len(v.Items))
		for i := range v.Items {
			out = append(out, &v.Items[i])
		}
		return out, nil

	case *gwv1alpha2.UDPRouteList:
		out := make([]client.Object, 0, len(v.Items))
		for i := range v.Items {
			out = append(out, &v.Items[i])
		}
		return out, nil

	default:
		return nil, fmt.Errorf("unsupported list type: %T", list)
	}
}

func (c *Command) writeGatewayObjects(directory string, objs []client.Object) error {
	for index, obj := range objs {
		ns := obj.GetNamespace()
		if ns == "" {
			ns = "cluster"
		}
		name := obj.GetName()

		filename := fmt.Sprintf("%d-%s-%s.yaml", index, ns, name)

		filename = safeFileName(filename)

		path := filepath.Join(directory, filename)

		// Convert to unstructured for sanitization
		raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			return fmt.Errorf("convert to unstructured failed (%s/%s): %w", ns, name, err)
		}

		sanitizeUnstructured(raw)

		enforceGatewayAPIVersion(raw)

		yml, err := yaml.Marshal(raw)
		if err != nil {
			return fmt.Errorf("yaml marshal failed for gateway api version(%s/%s): %w", ns, name, err)
		}
		if err := os.WriteFile(path, yml, 0644); err != nil {
			return fmt.Errorf("write failed for gateway api version(%s): %w", path, err)
		}
	}
	fmt.Printf("✅ Gateway API objects dumped into: %s\n", directory)
	return nil
}

func (c *Command) writeConsulObjects(directory string, objs []client.Object) error {
	for index, obj := range objs {
		ns := obj.GetNamespace()
		if ns == "" {
			ns = "cluster"
		}
		name := obj.GetName()

		filename := fmt.Sprintf("%d-%s-%s.yaml", index, ns, name)

		filename = safeFileName(filename)

		path := filepath.Join(directory, filename)

		// Convert to unstructured for sanitization
		raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			return fmt.Errorf("convert to unstructured failed (%s/%s): %w", ns, name, err)
		}

		sanitizeUnstructured(raw)

		if !enforceConsulApiVersion(raw) {
			continue
		}

		yml, err := yaml.Marshal(raw)
		if err != nil {
			return fmt.Errorf("yaml marshal failed for consul api version(%s/%s): %w", ns, name, err)
		}
		if err := os.WriteFile(path, yml, 0644); err != nil {
			return fmt.Errorf("write failed for consul api version(%s): %w", path, err)
		}

	}
	fmt.Printf("✅ Consul API objects dumped into: %s\n", directory)
	return nil
}

func (c *Command) writeObjects(kindDir string, objs []client.Object) error {

	gatewayAPIDir := filepath.Join(c.flagManifestsGatewayAPIDir, kindDir)
	if err := os.MkdirAll(gatewayAPIDir, 0755); err != nil {
		return err
	}

	if err := c.writeGatewayObjects(gatewayAPIDir, objs); err != nil {
		return err
	}

	var consulDir string
	// call this function only when consulApiEnabled is true; This generates another set of manifests for consul.hashicorp.com API group.
	// make another copy of raw to update the apiVersion for consul.hashicorp.com CRDs without affecting the gateway.networking.k8s.io versions

	if c.consulApiEnabled {

		consulDir = filepath.Join(c.flagManifestsConsulAPIDir, kindDir)

		if err := os.MkdirAll(consulDir, 0755); err != nil {
			return err
		}
		if err := c.writeConsulObjects(consulDir, objs); err != nil {
			return err
		}
	}
	return nil

}

func sanitizeUnstructured(obj map[string]interface{}) {
	// drop status (makes re-apply easier)
	delete(obj, "status")

	// clean metadata noise
	meta, ok := obj["metadata"].(map[string]interface{})
	if !ok {
		return
	}
	delete(meta, "managedFields")
	delete(meta, "resourceVersion")
	delete(meta, "uid")
	delete(meta, "generation")
	delete(meta, "creationTimestamp")

}

func safeFileName(s string) string {
	// makes filenames safe across OS/filesystems
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, ":", "_")
	return s
}

func (c *Command) validateFlags() error {

	if c.flagChart == "" {
		return errors.New("-chart must be set")
	}
	if c.flagApp == "" {
		return errors.New("-app must be set")
	}
	if c.flagRelease == "" {
		return errors.New("-release-name must be set")
	}
	if c.flagManifestsGatewayAPIDir == "" {
		return errors.New("-manifests-gatewayapi-dir must be set")
	}

	return nil
}

func deepCopyMap(in map[string]interface{}) map[string]interface{} {

	out := make(map[string]interface{}, len(in))

	for k, v := range in {

		switch val := v.(type) {

		case map[string]interface{}:
			out[k] = deepCopyMap(val)

		case []interface{}:
			out[k] = deepCopySlice(val)

		default:
			out[k] = val
		}
	}

	return out
}

func deepCopySlice(in []interface{}) []interface{} {

	out := make([]interface{}, len(in))

	for i, v := range in {

		switch val := v.(type) {

		case map[string]interface{}:
			out[i] = deepCopyMap(val)

		case []interface{}:
			out[i] = deepCopySlice(val)

		default:
			out[i] = val
		}
	}

	return out
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const (
	synopsis = "Generates manifests for gateway.networking.k8s.io API under specified directory , default: under consul/config under pre-upgrade condition only."
	help     = `
Usage: consul-k8s-control-plane generate-manifests [options]

 Generates manifests for gateway.networking.k8s.io API under specified directory , default: under consul/config under pre-upgrade condition only.

`
)
