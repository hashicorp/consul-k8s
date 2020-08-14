/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

//import (
//	"flag"
//	"io/ioutil"
//	"os"
//	"strings"
//
//	"github.com/hashicorp/consul/api"
//	"k8s.io/apimachinery/pkg/runtime"
//	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
//	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
//	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
//	ctrl "sigs.k8s.io/controller-runtime"
//	"sigs.k8s.io/controller-runtime/pkg/log/zap"
//
//	"github.com/hashicorp/consul-k8s/api/v1alpha1"
//	"github.com/hashicorp/consul-k8s/controllers"
//	// +kubebuilder:scaffold:imports
//)
//
//var (
//	scheme   = runtime.NewScheme()
//	setupLog = ctrl.Log.WithName("setup")
//)
//
//func init() {
//	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
//	utilruntime.Must(v1alpha1.AddToScheme(scheme))
//	// +kubebuilder:scaffold:scheme
//}
//
////
////func main() {
////	os.Exit(realMain())
////}
//
//func realMain() int {
//	var metricsAddr string
//	var enableLeaderElection bool
//	flags := flag.NewFlagSet("", flag.ContinueOnError)
//	flags.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
//	flags.BoolVar(&enableLeaderElection, "enable-leader-election", false,
//		"Enable leader election for controller manager. "+
//			"Enabling this will ensure there is only one active controller manager.")
//	httpFlags := HTTPFlags{}
//	Merge(flags, httpFlags.Flags())
//	if err := flags.Parse(nil); err != nil {
//		setupLog.Error(err, "parsing flags")
//		return 1
//	}
//
//	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
//
//	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
//		Scheme:             scheme,
//		MetricsBindAddress: metricsAddr,
//		Port:               9443,
//		LeaderElection:     enableLeaderElection,
//		LeaderElectionID:   "65a0bb41.my.domain",
//	})
//	if err != nil {
//		setupLog.Error(err, "unable to start manager")
//		return 1
//	}
//
//	consulClient, err := httpFlags.APIClient()
//	if err != nil {
//		setupLog.Error(err, "connecting to Consul agent")
//		return 1
//	}
//
//	if err = (&controllers.ServiceDefaultsReconciler{
//		Client:       mgr.GetClient(),
//		Log:          ctrl.Log.WithName("controllers").WithName("ServiceDefaults"),
//		Scheme:       mgr.GetScheme(),
//		ConsulClient: consulClient,
//	}).SetupWithManager(mgr); err != nil {
//		setupLog.Error(err, "unable to create controller", "controller", "ServiceDefaults")
//		return 1
//	}
//	// todo: this is super hacky. Setting global variable so the webhook validation can use the clients.
//	// Instead we should implement our own validating webhooks so we can pass in the clients.
//	v1alpha1.ConsulClient = consulClient
//	v1alpha1.KubeClient = mgr.GetClient()
//	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
//		if err = (&v1alpha1.ServiceDefaults{}).SetupWebhookWithManager(mgr); err != nil {
//			setupLog.Error(err, "unable to create webhook", "webhook", "ServiceDefaults")
//			os.Exit(1)
//		}
//	}
//	// +kubebuilder:scaffold:builder
//
//	setupLog.Info("starting manager")
//	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
//		setupLog.Error(err, "problem running manager")
//		return 1
//	}
//	return 0
//}
//
//// Taken from https://github.com/hashicorp/consul/blob/b5b9c8d953cd3c79c6b795946839f4cf5012f507/command/flags/http.go
//// with flags we don't use removed. This was done so we don't depend on internal
//// Consul implementation.
//
//// HTTPFlags are flags used to configure communication with a Consul agent.
//type HTTPFlags struct {
//	address       StringValue
//	token         StringValue
//	tokenFile     StringValue
//	caFile        StringValue
//	caPath        StringValue
//	certFile      StringValue
//	keyFile       StringValue
//	tlsServerName StringValue
//}
//
//func (f *HTTPFlags) Flags() *flag.FlagSet {
//	fs := flag.NewFlagSet("", flag.ContinueOnError)
//	fs.Var(&f.address, "http-addr",
//		"The `address` and port of the Consul HTTP agent. The value can be an IP "+
//			"address or DNS address, but it must also include the port. This can "+
//			"also be specified via the CONSUL_HTTP_ADDR environment variable. The "+
//			"default value is http://127.0.0.1:8500. The scheme can also be set to "+
//			"HTTPS by setting the environment variable CONSUL_HTTP_SSL=true.")
//	fs.Var(&f.token, "token",
//		"ACL token to use in the request. This can also be specified via the "+
//			"CONSUL_HTTP_TOKEN environment variable. If unspecified, the query will "+
//			"default to the token of the Consul agent at the HTTP address.")
//	fs.Var(&f.tokenFile, "token-file",
//		"File containing the ACL token to use in the request instead of one specified "+
//			"via the -token argument or CONSUL_HTTP_TOKEN environment variable. "+
//			"This can also be specified via the CONSUL_HTTP_TOKEN_FILE environment variable.")
//	fs.Var(&f.caFile, "ca-file",
//		"Path to a CA file to use for TLS when communicating with Consul. This "+
//			"can also be specified via the CONSUL_CACERT environment variable.")
//	fs.Var(&f.caPath, "ca-path",
//		"Path to a directory of CA certificates to use for TLS when communicating "+
//			"with Consul. This can also be specified via the CONSUL_CAPATH environment variable.")
//	fs.Var(&f.certFile, "client-cert",
//		"Path to a client cert file to use for TLS when 'verify_incoming' is enabled. This "+
//			"can also be specified via the CONSUL_CLIENT_CERT environment variable.")
//	fs.Var(&f.keyFile, "client-key",
//		"Path to a client key file to use for TLS when 'verify_incoming' is enabled. This "+
//			"can also be specified via the CONSUL_CLIENT_KEY environment variable.")
//	fs.Var(&f.tlsServerName, "tls-server-name",
//		"The server name to use as the SNI host when connecting via TLS. This "+
//			"can also be specified via the CONSUL_TLS_SERVER_NAME environment variable.")
//	return fs
//}
//
//func (f *HTTPFlags) Addr() string {
//	return f.address.String()
//}
//
//func (f *HTTPFlags) Token() string {
//	return f.token.String()
//}
//
//func (f *HTTPFlags) SetToken(v string) error {
//	return f.token.Set(v)
//}
//
//func (f *HTTPFlags) TokenFile() string {
//	return f.tokenFile.String()
//}
//
//func (f *HTTPFlags) SetTokenFile(v string) error {
//	return f.tokenFile.Set(v)
//}
//
//func (f *HTTPFlags) ReadTokenFile() (string, error) {
//	tokenFile := f.tokenFile.String()
//	if tokenFile == "" {
//		return "", nil
//	}
//
//	data, err := ioutil.ReadFile(tokenFile)
//	if err != nil {
//		return "", err
//	}
//
//	return strings.TrimSpace(string(data)), nil
//}
//
//func (f *HTTPFlags) APIClient() (*api.Client, error) {
//	c := api.DefaultConfig()
//
//	f.MergeOntoConfig(c)
//
//	return api.NewClient(c)
//}
//
//func (f *HTTPFlags) MergeOntoConfig(c *api.Config) {
//	f.address.Merge(&c.Address)
//	f.token.Merge(&c.Token)
//	f.tokenFile.Merge(&c.TokenFile)
//	f.caFile.Merge(&c.TLSConfig.CAFile)
//	f.caPath.Merge(&c.TLSConfig.CAPath)
//	f.certFile.Merge(&c.TLSConfig.CertFile)
//	f.keyFile.Merge(&c.TLSConfig.KeyFile)
//	f.tlsServerName.Merge(&c.TLSConfig.Address)
//}
//
//func Merge(dst, src *flag.FlagSet) {
//	if dst == nil {
//		panic("dst cannot be nil")
//	}
//	if src == nil {
//		return
//	}
//	src.VisitAll(func(f *flag.Flag) {
//		dst.Var(f.Value, f.Name, f.Usage)
//	})
//}
//
//// StringValue provides a flag value that's aware if it has been set.
//type StringValue struct {
//	v *string
//}
//
//// Merge will overlay this value if it has been set.
//func (s *StringValue) Merge(onto *string) {
//	if s.v != nil {
//		*onto = *(s.v)
//	}
//}
//
//// Set implements the flag.Value interface.
//func (s *StringValue) Set(v string) error {
//	if s.v == nil {
//		s.v = new(string)
//	}
//	*(s.v) = v
//	return nil
//}
//
//// String implements the flag.Value interface.
//func (s *StringValue) String() string {
//	var current string
//	if s.v != nil {
//		current = *(s.v)
//	}
//	return current
//}
