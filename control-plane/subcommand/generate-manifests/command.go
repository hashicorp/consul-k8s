// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package generatemanifests

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

const (
	gatewayConfigFilename  = "/consul/config/config.yaml"
	resourceConfigFilename = "/consul/config/resources.json"
)

type Command struct {
	UI cli.Ui

	flags *flag.FlagSet
	k8s   *flags.K8SFlags

	flagChart                  string
	flagApp                    string
	flagRelease                string
	flagManifestsGatewayAPIDir string

	flagOpenshiftSCCName string

	k8sClient client.Client

	once sync.Once
	help string

	nodeSelector       map[string]string
	tolerations        []corev1.Toleration
	serviceAnnotations []string
	resources          corev1.ResourceRequirements

	ctx context.Context
}

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

	c.flags.StringVar(
		&c.flagManifestsGatewayAPIDir,
		"manifests-gatewayapi-dir",
		"/output/gatewayapi",
		"Directory where Gateway API objects will be dumped.",
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
	// Fetch all the resources using the gateway.networking.k8s.io API in the cluster
	if err := c.dumpGatewayAPIObjects(); err != nil {
		c.UI.Error(fmt.Sprintf("Error dumping Gateway API objects: %s", err))
		return 1
	}
	time.Sleep(20 * time.Second)
	// delete gateway.networking.k8s.io crds
	// if err := c.deleteGatewayAPICRDs(); err != nil {
	// 	c.UI.Error(fmt.Sprintf("Error deleting Gateway API CRDs: %s", err))
	// 	return 1
	// }
	c.UI.Info(fmt.Sprintf("✅ Gateway API objects dumped into: %s", c.flagManifestsGatewayAPIDir))
	return 0

}

func (c *Command) deleteGatewayAPICRDs() error {
	crds := []string{
		"gatewayclasses.gateway.networking.k8s.io",
		"gateways.gateway.networking.k8s.io",
		"httproutes.gateway.networking.k8s.io",
		"referencegrants.gateway.networking.k8s.io",
		"grpcroutes.gateway.networking.k8s.io",
		"tcproutes.gateway.networking.k8s.io",
		"tlsroutes.gateway.networking.k8s.io",
		"udproutes.gateway.networking.k8s.io",
	}

	for _, crd := range crds {
		err := c.k8sClient.Delete(c.ctx, &apiextensions.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Name: crd,
			},
		},
		)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error deleting CRD %s: %s", crd, err))
			continue
		}

		c.UI.Info(fmt.Sprintf("✅ Deleted CRD: %s", crd))
	}
	c.UI.Info("✅ Deleted all Gateway API CRDs")
	return nil
}

func (c *Command) dumpGatewayAPIObjects() error {
	if c.k8sClient == nil {
		return fmt.Errorf("k8s client is nil")
	}

	// Ensure base output dir exists
	if err := os.MkdirAll(c.flagManifestsGatewayAPIDir, 0755); err != nil {
		return err
	}

	ctx := c.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// Dump resources
	if err := c.dumpTypedList(ctx, "gatewayclasses", &gwv1beta1.GatewayClassList{}); err != nil {
		c.UI.Info(fmt.Sprintf("Skipping GatewayClass dump: %v", err))
	}

	if err := c.dumpTypedList(ctx, "gateways", &gwv1beta1.GatewayList{}); err != nil {
		c.UI.Info(fmt.Sprintf("Skipping Gateway dump: %v", err))
	}

	if err := c.dumpTypedList(ctx, "httproutes", &gwv1beta1.HTTPRouteList{}); err != nil {
		c.UI.Info(fmt.Sprintf("Skipping HTTPRoute dump: %v", err))
	}

	if err := c.dumpTypedList(ctx, "grpcroutes", &gwv1alpha2.GRPCRouteList{}); err != nil {
		c.UI.Info(fmt.Sprintf("Skipping GRPCRoute dump: %v", err))
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
func enforceGatewayAPIVersion(raw map[string]interface{}) {
	kind, _ := raw["kind"].(string)
	if kind == "" {
		return
	}

	switch kind {

	// you asked these to be gateway.networking.k8s.io/v1
	case "GatewayClass", "Gateway", "HTTPRoute", "GRPCRoute":
		raw["apiVersion"] = "gateway.networking.k8s.io/v1"

	// ReferenceGrant -> v1beta1
	case "ReferenceGrant":
		raw["apiVersion"] = "gateway.networking.k8s.io/v1beta1"

	// UDP/TLS/TCP -> v1alpha2
	case "UDPRoute", "TLSRoute", "TCPRoute":
		raw["apiVersion"] = "gateway.networking.k8s.io/v1alpha2"
	}
}

func extractItems(list client.ObjectList) ([]client.Object, error) {
	switch v := list.(type) {
	case *gwv1beta1.GatewayClassList:
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

	case *gwv1beta1.HTTPRouteList:
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

// func (c *Command) dumpUnstructuredList(ctx context.Context, kindDir, group, version, kind string) error {
// 	u := &unstructured.UnstructuredList{}
// 	u.SetAPIVersion(group + "/" + version)
// 	u.SetKind(kind)

// 	if err := c.k8sClient.List(ctx, u); err != nil {
// 		return err
// 	}

// 	items := make([]client.Object, 0, len(u.Items))
// 	for i := range u.Items {
// 		// must take address of element
// 		obj := u.Items[i]
// 		items = append(items, &obj)
// 	}

// 	return c.writeObjects(kindDir, items)
// }

func (c *Command) writeObjects(kindDir string, objs []client.Object) error {
	dir := filepath.Join(c.flagManifestsGatewayAPIDir, kindDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	for index, obj := range objs {
		ns := obj.GetNamespace()
		if ns == "" {
			ns = "cluster"
		}
		name := obj.GetName()

		filename := fmt.Sprintf("%d-%s-%s.yaml", index, ns, name)

		filename = safeFileName(filename)

		path := filepath.Join(dir, filename)

		// Convert to unstructured for sanitization
		raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			return fmt.Errorf("convert to unstructured failed (%s/%s): %w", ns, name, err)
		}

		sanitizeUnstructured(raw)
		enforceGatewayAPIVersion(raw)

		yml, err := yaml.Marshal(raw)
		if err != nil {
			return fmt.Errorf("yaml marshal failed (%s/%s): %w", ns, name, err)
		}
		// update the apiVersion to remove k8s.io specific versions

		if err := os.WriteFile(path, yml, 0644); err != nil {
			return fmt.Errorf("write failed (%s): %w", path, err)
		}
	}

	c.UI.Info(fmt.Sprintf("✅ dumped %d objects into %s", len(objs), dir))
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

	// annotations sometimes too noisy (optional)
	// if ann, ok := meta["annotations"].(map[string]interface{}); ok {
	// 	delete(ann, "kubectl.kubernetes.io/last-applied-configuration")
	// }
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
