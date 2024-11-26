package read

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	helmcli "helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	"sigs.k8s.io/yaml"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
)

type Command struct {
	*common.BaseCommand

	kubernetes client.Client
	restConfig *rest.Config

	set *flag.Sets

	flagGatewayKind      string
	flagGatewayNamespace string
	flagKubeConfig       string
	flagKubeContext      string
	flagOutput           string

	gatewayName string

	initOnce sync.Once
	help     string
}

func (c *Command) Help() string {
	c.initOnce.Do(c.init)
	return fmt.Sprintf("%s\n\nUsage: consul-k8s gateway read <gateway-name> [flags]\n\n%s", c.Synopsis(), c.help)
}

func (c *Command) Synopsis() string {
	return "Inspect the configuration for a given Gateway."
}

// init establishes the flags for Command
func (c *Command) init() {
	c.set = flag.NewSets()

	f := c.set.NewSet("Command Options")
	f.StringVar(&flag.StringVar{
		Name:    "namespace",
		Target:  &c.flagGatewayNamespace,
		Usage:   "The namespace of the Gateway to read",
		Aliases: []string{"n"},
	})
	f.StringVar(&flag.StringVar{
		Name:    "output",
		Target:  &c.flagOutput,
		Usage:   "Output the Gateway configuration as 'json' in the terminal or 'archive' as a zip archive in the current directory named after the Gateway.",
		Default: "archive",
		Aliases: []string{"o"},
	})

	f = c.set.NewSet("Global Options")
	f.StringVar(&flag.StringVar{
		Name:    "kubeconfig",
		Aliases: []string{"c"},
		Target:  &c.flagKubeConfig,
		Usage:   "Set the path to kubeconfig file.",
	})
	f.StringVar(&flag.StringVar{
		Name:   "context",
		Target: &c.flagKubeContext,
		Usage:  "Set the Kubernetes context to use.",
	})

	c.help = c.set.Help()
}

// Run runs the command
func (c *Command) Run(args []string) int {
	c.initOnce.Do(c.init)
	c.Log.ResetNamed("read")
	defer common.CloseWithError(c.BaseCommand)

	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		c.UI.Output("Usage: gateway read <gateway-name>")
		return 1
	}

	if len(args) > 1 {
		if err := c.set.Parse(args[1:]); err != nil {
			c.UI.Output(err.Error(), terminal.WithErrorStyle())
			return 1
		}
	}

	c.gatewayName = args[0]

	if err := c.initKubernetes(); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	if err := c.fetchCRDs(); err != nil {
		c.UI.Output(err.Error(), terminal.WithErrorStyle())
		return 1
	}

	return 0
}

func (c *Command) fetchCRDs() error {
	// Fetch Gateway
	var gateway gwv1beta1.Gateway
	if err := c.kubernetes.Get(context.Background(), client.ObjectKey{Namespace: c.flagGatewayNamespace, Name: c.gatewayName}, &gateway); err != nil {
		return fmt.Errorf("error fetching Gateway CRD: %w", err)
	}

	// Fetch GatewayClass referenced by Gateway
	var gatewayClass gwv1beta1.GatewayClass
	if err := c.kubernetes.Get(context.Background(), client.ObjectKey{Namespace: "", Name: string(gateway.Spec.GatewayClassName)}, &gatewayClass); err != nil {
		return fmt.Errorf("error fetching GatewayClass CRD: %w", err)
	}

	//// Fetch GatewayClassConfig referenced by GatewayClass
	//var gatewayClassConfig v1alpha1.GatewayClassConfig
	//if err := c.kubernetes.Get(context.Background(), client.ObjectKey{Namespace: "", Name: gatewayClass.Spec.ParametersRef.Name}, &gatewayClassConfig); err != nil {
	//	return fmt.Errorf("error fetching GatewayClassConfig CRD: %w", err)
	//}

	// Fetch HTTPRoutes that reference the Gateway
	var httpRoutes gwv1beta1.HTTPRouteList
	if err := c.kubernetes.List(context.Background(), &httpRoutes); err != nil {
		return fmt.Errorf("error fetching HTTPRoute CRDs: %w", err)
	}

	// Fetch TCPRoutes that reference the Gateway
	var tcpRoutes gwv1alpha2.TCPRouteList
	if err := c.kubernetes.List(context.Background(), &tcpRoutes); err != nil {
		return fmt.Errorf("error fetching TCPRoute CRDs: %w", err)
	}

	// Fetch MeshServices referenced by HTTPRoutes or TCPRoutes
	// FUTURE Filter to those referenced by HTTPRoutes or TCPRoutes instead of listing all
	// var meshServices v1alpha1.MeshServiceList
	// if err := c.kubernetes.List(context.Background(), &meshServices); err != nil {
	//   return fmt.Errorf("error fetching MeshService CRDs: %w", err)
	// }

	gatewayWithRoutes := struct {
		Gateway      gwv1beta1.Gateway      `json:"gateway"`
		GatewayClass gwv1beta1.GatewayClass `json:"gatewayClass"`
		HTTPRoutes   []gwv1beta1.HTTPRoute  `json:"httpRoutes"`
		TCPRoutes    []gwv1alpha2.TCPRoute  `json:"tcpRoutes"`
	}{
		Gateway:      gateway,
		GatewayClass: gatewayClass,
		HTTPRoutes:   make([]gwv1beta1.HTTPRoute, 0, len(httpRoutes.Items)),
		TCPRoutes:    make([]gwv1alpha2.TCPRoute, 0, len(tcpRoutes.Items)),
	}

	for _, route := range httpRoutes.Items {
		for _, ref := range route.Spec.ParentRefs {
			switch {
			case string(ref.Name) != gateway.Name:
				// Route parent references gateway with different name
				continue
			case ref.Namespace != nil && string(*ref.Namespace) == gateway.Namespace:
				// Route parent explicitly references gateway with same name and namespace
				gatewayWithRoutes.HTTPRoutes = append(gatewayWithRoutes.HTTPRoutes, route)
			case ref.Namespace == nil && route.Namespace == gateway.Namespace:
				// Route parent implicitly references gateway with same name in local namespace
				gatewayWithRoutes.HTTPRoutes = append(gatewayWithRoutes.HTTPRoutes, route)
			}
		}
	}

	for _, route := range tcpRoutes.Items {
		for _, ref := range route.Spec.ParentRefs {
			switch {
			case string(ref.Name) != gateway.Name:
				// Route parent references gateway with different name
				continue
			case ref.Namespace != nil && string(*ref.Namespace) == gateway.Namespace:
				// Route parent explicitly references gateway with same name and namespace
				gatewayWithRoutes.TCPRoutes = append(gatewayWithRoutes.TCPRoutes, route)
			case ref.Namespace == nil && route.Namespace == gateway.Namespace:
				// Route parent implicitly references gateway with same name in local namespace
				gatewayWithRoutes.TCPRoutes = append(gatewayWithRoutes.TCPRoutes, route)
			}
		}
	}

	switch strings.ToLower(c.flagOutput) {
	case "json":
		if err := c.writeJSONOutput(gatewayWithRoutes); err != nil {
			return fmt.Errorf("error writing CRDs as JSON: %w", err)
		}
	default:
		file, err := os.Create(fmt.Sprintf("./%s.zip", c.gatewayName))
		if err != nil {
			return fmt.Errorf("error creating output file: %w", err)
		}

		zipw := zip.NewWriter(file)
		defer zipw.Close()

		if err := c.writeArchive(zipw, c.gatewayName+".yaml", gatewayWithRoutes); err != nil {
			return fmt.Errorf("error writing CRDs to zip archive: %w", err)
		}
		return zipw.Close()
	}

	return nil
}

func (c *Command) writeJSONOutput(obj interface{}) error {
	output, err := json.MarshalIndent(obj, "", "\t")
	if err != nil {
		return err
	}

	c.UI.Output(string(output))
	return nil
}

func (c *Command) writeArchive(zipw *zip.Writer, name string, obj interface{}) error {
	w, err := zipw.Create(name)
	if err != nil {
		return fmt.Errorf("error creating zip entry for %s: %w", name, err)
	}

	objYaml, err := yaml.Marshal(obj)
	if err != nil {
		return fmt.Errorf("error marshalling %s: %w", name, err)
	}

	_, err = w.Write(objYaml)
	if err != nil {
		return fmt.Errorf("error writing %s to zip archive: %w", name, err)
	}

	c.UI.Output("Wrote to zip archive " + name)

	return nil
}

// initKubernetes initializes the REST config and uses it to initialize the k8s client.
func (c *Command) initKubernetes() (err error) {
	settings := helmcli.New()

	// If a kubeconfig was specified, use it
	if c.flagKubeConfig != "" {
		settings.KubeConfig = c.flagKubeConfig
	}

	// If a kube context was specified, use it
	if c.flagKubeContext != "" {
		settings.KubeContext = c.flagKubeContext
	}

	// Create a REST config from the settings for our Kubernetes client
	if c.restConfig == nil {
		if c.restConfig, err = settings.RESTClientGetter().ToRESTConfig(); err != nil {
			return fmt.Errorf("error creating Kubernetes REST config: %w", err)
		}
	}

	// Create a controller-runtime client from c.restConfig
	if c.kubernetes == nil {
		if c.kubernetes, err = client.New(c.restConfig, client.Options{}); err != nil {
			return fmt.Errorf("error creating controller-runtime client: %w", err)
		}
		// TODO Fix dependency issues introduced by registering v1alpha1
		// _ = v1alpha1.AddToScheme(c.kubernetes.Scheme())
		_ = gwv1alpha2.AddToScheme(c.kubernetes.Scheme())
		_ = gwv1beta1.AddToScheme(c.kubernetes.Scheme())
	}

	// If no namespace was specified, use the one from the kube context
	if c.flagGatewayNamespace == "" {
		c.UI.Output("No namespace specified, using current kube context namespace: %s", settings.Namespace())
		c.flagGatewayNamespace = settings.Namespace()
	}

	return nil
}
