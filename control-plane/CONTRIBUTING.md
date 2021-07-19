# Contributing

To build and install `consul-k8s` locally, Go version 1.11.4+ is required because this repository uses go modules and go 1.11.4 introduced changes to checksumming of modules to correct a symlink problem.
You will also need to install the Docker engine:

- [Docker for Mac](https://docs.docker.com/engine/installation/mac/)
- [Docker for Windows](https://docs.docker.com/engine/installation/windows/)
- [Docker for Linux](https://docs.docker.com/engine/installation/linux/ubuntulinux/)

Clone the repository:

```shell
$ git clone https://github.com/hashicorp/consul-k8s.git
```

To compile the `consul-k8s` binary for your local machine:

```shell
$ make dev
```

This will compile the `consul-k8s` binary into `bin/consul-k8s` as
well as your `$GOPATH` and run the test suite.

Or run the following to generate all binaries:

```shell
$ make dist
```

If you just want to run the tests:

```shell
$ make test
```

Or to run a specific test in the suite:

```shell
go test ./... -run SomeTestFunction_name
```

To create a docker image with your local changes:

```shell
$ make dev-docker
```

### Rebasing contributions against master

PRs in this repo are merged using the [`rebase`](https://git-scm.com/docs/git-rebase) method. This keeps
the git history clean by adding the PR commits to the most recent end of the commit history. It also has
the benefit of keeping all the relevant commits for a given PR together, rather than spread throughout the
git history based on when the commits were first created.

If the changes in your PR do not conflict with any of the existing code in the project, then Github supports
automatic rebasing when the PR is accepted into the code. However, if there are conflicts (there will be
a warning on the PR that reads "This branch cannot be rebased due to conflicts"), you will need to manually
rebase the branch on master, fixing any conflicts along the way before the code can be merged.

## Creating a new CRD

### The Structs
1. Run the generate command:
    ```bash
    operator-sdk create api --group consul --version v1alpha1 --kind IngressGateway --controller --namespaced=true --make=false --resource=true
    ```
1. Re-order the file so it looks like:
    ```go
    func init() {
    	SchemeBuilder.Register(&IngressGateway{}, &IngressGatewayList{})
    }
    
    // +kubebuilder:object:root=true
    // +kubebuilder:subresource:status
    
    // IngressGateway is the Schema for the ingressgateways API
    type IngressGateway struct {
    	metav1.TypeMeta   `json:",inline"`
    	metav1.ObjectMeta `json:"metadata,omitempty"`
    
    	Spec   IngressGatewaySpec   `json:"spec,omitempty"`
    	Status IngressGatewayStatus `json:"status,omitempty"`
    }
    
    // +kubebuilder:object:root=true
    
    // IngressGatewayList contains a list of IngressGateway
    type IngressGatewayList struct {
    	metav1.TypeMeta `json:",inline"`
    	metav1.ListMeta `json:"metadata,omitempty"`
    	Items           []IngressGateway `json:"items"`
    }
    
    // IngressGatewaySpec defines the desired state of IngressGateway
    type IngressGatewaySpec struct {
    	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
    	// Important: Run "make" to regenerate code after modifying this file
    
    	// Foo is an example field of IngressGateway. Edit IngressGateway_types.go to remove/update
    	Foo string `json:"foo,omitempty"`
    }
    
    // IngressGatewayStatus defines the observed state of IngressGateway
    type IngressGatewayStatus struct {
    	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
    	// Important: Run "make" to regenerate code after modifying this file
    }
    ```
1. Add kubebuilder status metadata to the `IngressGateway struct`:
    ```go
    // ServiceRouter is the Schema for the servicerouters API
    // +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
    // +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
    type ServiceRouter struct {
    ```
1. Delete `IngressGatewayStatus` struct. We use a common status struct.
1. Use the `Status` struct instead and embed it:
    ```diff
    // IngressGateway is the Schema for the ingressgateways API
    type IngressGateway struct {
    	metav1.TypeMeta   `json:",inline"`
    	metav1.ObjectMeta `json:"metadata,omitempty"`
    
    	Spec   IngressGatewaySpec   `json:"spec,omitempty"`
    -	Status IngressGatewayStatus `json:"status,omitempty"`
    +	Status `json:"status,omitempty"`
    }
    ```
1. Go to the Consul `api` package for the config entry, e.g. https://github.com/hashicorp/consul/blob/master/api/config_entry_gateways.go
1. Copy the top-level fields over into the `Spec` struct except for
   `Kind`, `Name`, `Namespace`, `Meta`, `CreateIndex` and `ModifyIndex`. In this
   example, the top-level fields remaining are `TLS` and `Listeners`:
   
    ```go
    // IngressGatewaySpec defines the desired state of IngressGateway
    type IngressGatewaySpec struct {
        // TLS holds the TLS configuration for this gateway.
        TLS GatewayTLSConfig
        // Listeners declares what ports the ingress gateway should listen on, and
        // what services to associated to those ports.
        Listeners []IngressListener
    }
    ```
1. Copy the structs over that are missing, e.g. `GatewayTLSConfig`, `IngressListener`.
1. Set `json` tags for all fields using camelCase starting with a lowercase letter:
    ```go
        TLS GatewayTLSConfig `json:"tls"`
    ```
   Note that you should use the fields name, e.g. `tls`, not the struct name, e.g. `gatewayTLSConfig`.
   Remove any `alias` struct tags.
1. If the fields aren't documented, document them using the Consul docs as a reference.

### Spec Methods
1. Run `make ctrl-generate` to implement the deep copy methods.
1. Implement all the methods for `ConfigEntryResource` in the `_types.go` file. If using goland you can
   automatically stub out all the methods by using Code -> Generate -> IngressGateway -> ConfigEntryResource.
1. Use existing implementations of other types to implement the methods. We have to
   copy their code because we can't use a common struct that implements the methods
   because that messes up the CRD code generation. 
   
   You should be able to follow the other "normal" types. The non-normal types
   are `ServiceIntention` and `ProxyDefault` because they have special behaviour
   around being global or their spec not matching up with Consul's directly.
1. When you get to `ToConsul` and `Validate` you'll need to actually think
   about the implementation instead of copy/pasting and doing a simple replace.
1. For `ToConsul`, the pattern we follow is to implement `toConsul()` methods
   on each sub-struct. You can see this pattern in the existing types.
1. For `Validate`, we again follow the pattern of implementing the method on
   each sub-struct. You'll need to read the Consul documentation to understand
   what validation needs to be done.
   
   Things to keep in mind:
   1. Re-use the `sliceContains` and `notInSliceMessage` helper methods where applicable.
   1. If the invalid field is an entire struct, encode as json (look for `asJSON` for an example).
   1. `validateNamespaces` should be a separate method.
   1. If the field can have a `nil` pointer, check for that, e.g.
        ```go
        func (in *ServiceRouteHTTPMatchHeader) validate(path *field.Path) *field.Error {
            if in == nil {
                return nil
            }
        ```

### Spec Tests
1. Create a test file, e.g. `ingressgateway_types_test.go`.
1. Copy the tests for the `ConfigEntryResource` methods from another type and search and replace.
   Only the tests for `ToConsul()`, `Validate()` and `MatchesConsul()` need to
   be implemented without copying.
1. The test for `MatchesConsul` will look like:
    ```go
    func TestIngressGateway_MatchesConsul(t *testing.T) {
        cases := map[string]struct {
            Ours    IngressGateway
            Theirs  capi.ConfigEntry
            Matches bool
        }{
            "empty fields matches": {
            "all fields set matches": {
            "different types does not match": {

        }

        for name, c := range cases {
            t.Run(name, func(t *testing.T) {
                require.Equal(t, c.Matches, c.Ours.MatchesConsul(c.Theirs))
            })
        }
    }
    ```
1. The test for `ToConsul` will re-use the same cases as for `MatchesConsul()`
   with the following modifications:
   1. The case with `empty field matches` will use the same struct, but the case will be renamed to `empty fields`
   1. The case with `all fields set matches` will be renamed to `every field set`
   1. All cases will remove the `Namespace` and `CreateIndex`/`ModifyIndex` fields
      since the `ToConsul` method won't set those
1. The test for `Validate` should exercise all the validations you wrote.

### Controller
1. Delete the file `controllers/suite_test.go`. We don't write suite tests, just unit tests.
1. Move `controllers/ingressgateway_controller.go` to `controller` directory.
1. Delete the `controllers` directory.
1. Rename `Reconciler` to `Controller`, e.g. `IngressGatewayReconciler` => `IngressGatewayController`
1. Use the existing controller files as a guide and make this file match.
1. Add your controller as a case in the tests in `configentry_controller_test.go`:
    1. `TestConfigEntryControllers_createsConfigEntry`
    1. `TestConfigEntryControllers_updatesConfigEntry`
    1. `TestConfigEntryControllers_deletesConfigEntry`
    1. `TestConfigEntryControllers_errorUpdatesSyncStatus`
    1. `TestConfigEntryControllers_setsSyncedToTrue`
    1. `TestConfigEntryControllers_doesNotCreateUnownedConfigEntry`
    1. `TestConfigEntryControllers_doesNotDeleteUnownedConfig`
1. Note: we don't add tests to `configentry_controller_ent_test.go` because we decided
   it's too much duplication and the controllers are already properly exercised in the oss tests. 

### Webhook
1. Copy an existing webhook to `api/v1alpha/ingressgateway_webhook.go`
1. Replace the names
1. Ensure you've correctly replaced the names in the kubebuilder annotation, ensure the plurality is correct
    ```go
    // +kubebuilder:webhook:verbs=create;update,path=/mutate-v1alpha1-ingressgateway,mutating=true,failurePolicy=fail,groups=consul.hashicorp.com,resources=ingressgateways,versions=v1alpha1,name=mutate-ingressgateway.consul.hashicorp.com,webhookVersions=v1beta1,sideEffects=None
    ```

### Update command.go
1. Add your resource name to `api/common/common.go`:
    ```go
    const (
        ...
        IngressGateway    string = "ingressgateway"
    ```
1. Update `subcommand/controller/command.go` and add your controller:
    ```go
    if err = (&controller.IngressGatewayController{
        ConfigEntryController: configEntryReconciler,
        Client:                mgr.GetClient(),
        Log:                   ctrl.Log.WithName("controller").WithName(common.IngressGateway),
        Scheme:                mgr.GetScheme(),
    }).SetupWithManager(mgr); err != nil {
        setupLog.Error(err, "unable to create controller", "controller", common.IngressGateway)
        return 1
    }
    ```
1. Update `subcommand/controller/command.go` and add your webhook (the path should match the kubebuilder annotation):
    ```go
    mgr.GetWebhookServer().Register("/mutate-v1alpha1-ingressgateway",
        &webhook.Admission{Handler: &v1alpha1.IngressGatewayWebhook{
            Client:                     mgr.GetClient(),
            ConsulClient:               consulClient,
            Logger:                     ctrl.Log.WithName("webhooks").WithName(common.IngressGateway),
            EnableConsulNamespaces:     c.flagEnableNamespaces,
            EnableNSMirroring:          c.flagEnableNSMirroring,
        }})
    ```

### Generating YAML
1. Run `make ctrl-manifests` to generate the CRD and webhook YAML.
1. Uncomment your CRD in `config/crd/kustomization` under `patchesStrategicMerge:`
1. Update the sample, e.g. `config/samples/consul_v1alpha1_ingressgateway.yaml` to a valid resource
   that can be used for testing:
    ```yaml
    apiVersion: consul.hashicorp.com/v1alpha1
    kind: IngressGateway
    metadata:
      name: ingressgateway-sample
    spec:
      tls:
        enabled: false
      listeners:
        - port: 8080
          protocol: "tcp"
          services:
            - name: "foo"
    ```

### Updating consul-helm
1. To copy the CRD YAML into the consul-helm repo, run `make ctrl-crd-copy helm=<path-to-consul-helm>`
    where `<path-to-consul-helm>` is the relative or absolute path to your consul-helm repository.
    This will copy your CRD into consul-helm with the required formatting.
1. In consul-helm, update `templates/controller-mutatingwebhookconfiguration` with the webhook for this resource
   using the updated `config/webhook/manifests.v1beta1.yaml` and replacing `clientConfig.service.name/namespace`
   with the templated strings shown below to match the other webhooks.:
    ```yaml
    - clientConfig:
        service:
          name: {{ template "consul.fullname" . }}-controller-webhook
          namespace: {{ .Release.Namespace }}
          path: /mutate-v1alpha1-ingressgateway
      failurePolicy: Fail
      admissionReviewVersions:
        - "v1beta1"
        - "v1"
      name: mutate-ingressgateway.consul.hashicorp.com
      rules:
        - apiGroups:
            - consul.hashicorp.com
          apiVersions:
            - v1alpha1
          operations:
            - CREATE
            - UPDATE
          resources:
            - ingressgateways
      sideEffects: None
    ```
1. Update `templates/controller-clusterrole.yaml` to allow the controller to
   manage your resource type.

### Test
1. Build a Docker image for consul-k8s via `make dev-docker` and tagging your image appropriately.
1. Install using the updated Helm repository, with a values like:
    ```yaml
    global:
      imageK8S: ghcr.io/lkysow/consul-k8s-dev:nov26
      name: consul
    server:
      replicas: 1
      bootstrapExpect: 1
    controller:
      enabled: true
    ```
1. `kubectl apply` your sample CRD.
1. Check its synced status:
    ```bash
    kubectl get ingressgateway
    NAME                    SYNCED   AGE
    ingressgateway-sample   True     8s
    ```
1. Make a call to consul to confirm it was created as expected:
    ```bash
    kubectl exec consul-server-0 -- consul config read -name ingressgateway-sample -kind ingress-gateway
    {
        "Kind": "ingress-gateway",
        "Name": "ingressgateway-sample",
        "TLS": {
            "Enabled": false
        },
        "Listeners": [
            {
                "Port": 8080,
                "Protocol": "tcp",
                "Services": [
                    {
                        "Name": "foo",
                        "Hosts": null
                    }
                ]
            }
        ],
        "Meta": {
            "consul.hashicorp.com/source-datacenter": "dc1",
            "external-source": "kubernetes"
        },
        "CreateIndex": 57,
        "ModifyIndex": 57
    }
    ```

### Update consul-helm Acceptance Tests
1. Add a test resource to `test/acceptance/tests/fixtures/crds/ingressgateway.yaml`. Ideally it requires
   no other resources. For example, I used a `tcp` service so it didn't require a `ServiceDefaults`
   resource to set its protocol to something else.
1. Update `test/acceptance/tests/controller/controller_test.go` and `test/acceptance/tests/controller/controller_namespaces_test.go`.
1. Test locally, then submit a PR that uses your Docker image as `global.imageK8S`.
