package read

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
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

	gatewayName string

	initOnce sync.Once
	help     string
}

func (c *Command) Help() string {
	return "TODO"
}

func (c *Command) Synopsis() string {
	return "TODO"
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
	c.UI.Output("Reading Gateway CRDs: %s/%s", c.flagGatewayNamespace, c.gatewayName)

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
	file, err := os.Create("./gateway.zip")
	if err != nil {
		return fmt.Errorf("error creating output file: %w", err)
	}

	zipw := zip.NewWriter(file)
	defer zipw.Close()

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

	var orphanedHTTPRoutes []gwv1beta1.HTTPRoute
	var orphanedTCPRoutes []gwv1alpha2.TCPRoute

nextHTTPRoute:
	for _, route := range httpRoutes.Items {
		for _, ref := range route.Spec.ParentRefs {
			switch {
			case string(ref.Name) != gateway.Name:
				// Route parent references gateway with different name
				c.Log.Warn("Route parent references gateway with different name: %s", ref.Name)
				continue
			case ref.Namespace != nil && string(*ref.Namespace) == gateway.Namespace:
				gatewayWithRoutes.HTTPRoutes = append(gatewayWithRoutes.HTTPRoutes, route)
				c.Log.Info("Route parent references gateway with same name and namespace", "route", route.Name)
				break nextHTTPRoute
			case ref.Namespace == nil && route.Namespace == gateway.Namespace:
				gatewayWithRoutes.HTTPRoutes = append(gatewayWithRoutes.HTTPRoutes, route)
				c.Log.Info("Route parent references gateway with same name and no namespace", "route", route.Name)
				break nextHTTPRoute
			}
		}

		// Route had no parent matching gateway
		orphanedHTTPRoutes = append(orphanedHTTPRoutes, route)
	}

nextTCPRoute:
	for _, route := range tcpRoutes.Items {
		for _, ref := range route.Spec.ParentRefs {
			switch {
			case string(ref.Name) != gateway.Name:
				// Route parent references gateway with different name
				c.Log.Warn("Route parent references gateway with different name: %s", ref.Name)
				continue
			case ref.Namespace != nil && string(*ref.Namespace) == gateway.Namespace:
				gatewayWithRoutes.TCPRoutes = append(gatewayWithRoutes.TCPRoutes, route)
				c.Log.Info("Route parent references gateway with same name and namespace", "route", route.Name)
				break nextTCPRoute
			case ref.Namespace == nil && route.Namespace == gateway.Namespace:
				gatewayWithRoutes.TCPRoutes = append(gatewayWithRoutes.TCPRoutes, route)
				c.Log.Info("Route parent references gateway with same name and no namespace", "route", route.Name)
				break nextTCPRoute
			}
		}

		// Route had no parent matching gateway
		orphanedTCPRoutes = append(orphanedTCPRoutes, route)
	}

	//// Fetch MeshServices referenced by HTTPRoutes or TCPRoutes
	//// TODO Filter to those referenced by HTTPRoutes or TCPRoutes instead of listing all
	//var meshServices v1alpha1.MeshServiceList
	//if err := c.kubernetes.List(context.Background(), &meshServices); err != nil {
	//	return fmt.Errorf("error fetching MeshService CRDs: %w", err)
	//}
	var eg errgroup.Group
	eg.SetLimit(1)

	eg.Go(func() error { return writeYamlFile(zipw, c.gatewayName+".yaml", gatewayWithRoutes) })

	if len(orphanedHTTPRoutes) > 0 {
		eg.Go(func() error { return writeYamlFile(zipw, "orphaned-http-routes.yaml", orphanedHTTPRoutes) })
	}

	if len(orphanedTCPRoutes) > 0 {
		eg.Go(func() error { return writeYamlFile(zipw, "orphaned-tcp-routes.yaml", orphanedTCPRoutes) })
	}

	//eg.Go(func() error { return writeYamlFile(zipw, "gateway.yaml", gateway) })
	//eg.Go(func() error { return writeYamlFile(zipw, "gatewayclass.yaml", gatewayClass) })
	////eg.Go(func() error { return writeYamlFile(zipw, "gatewayclassconfig.yaml", gatewayClassConfig) })
	//eg.Go(func() error { return writeYamlFile(zipw, "httproutes.yaml", httpRoutes) })
	//eg.Go(func() error { return writeYamlFile(zipw, "tcproutes.yaml", tcpRoutes) })
	//eg.Go(func() error { return writeYamlFile(zipw, "meshservices.yaml", meshServices) })

	if err := eg.Wait(); err != nil {
		return fmt.Errorf("error writing CRDs to zip archive: %w", err)
	}

	return zipw.Close()
}

func writeYamlFile(zipw *zip.Writer, name string, obj interface{}) error {
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

	return nil
}

// initKubernetes initializes the REST config and uses it to initialize the k8s client.
// TODO support namespace, context, etc. flags
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
