# Contributing to Consul on Kubernetes

1. [Contributing 101](#contributing-101)
    1. [Building and running `consul-k8s-control-plane`](#building-and-running-consul-k8s-control-plane)
    1. [Building and running the `consul-k8s` CLI](#building-and-running-the-consul-k8s-cli)
    1. [Making changes to consul-k8s](#making-changes-to-consul-k8s)
    1. [Running linters locally](#running-linters-locally)
    1. [Rebasing contributions against main](#rebasing-contributions-against-main)
1. [Creating a new CRD](#creating-a-new-crd)
    1. [The Structs](#the-structs)
    1. [Spec Methods](#spec-methods)
    1. [Spec Tests](#spec-tests)
    1. [Controller](#controller)
    1. [Webhook](#webhook)
    1. [Update command.go](#update-commandgo)
    1. [Generating YAML](#generating-yaml)
    1. [Updating consul-helm](#updating-helm-chart)
    1. [Testing a new CRD](#testing-a-new-crd)
    1. [Update Consul K8s acceptance tests](#update-consul-k8s-acceptance-tests)
1. [Adding a new ACL Token](#adding-a-new-acl-token)
1. [Testing the Helm chart](#testing-the-helm-chart)
    1. [Running the tests](#running-the-tests)
    1. [Writing Unit tests](#writing-unit-tests)
    1. [Writing Acceptance tests](#writing-acceptance-tests)
1. [Using the Acceptance Test Framework to Debug](#using-acceptance-test-framework-to-debug)
1. [Helm Reference Docs](#helm-reference-docs)
1. [Managing External CRD Dependencies](#managing-external-crd-dependencies)
1. [Adding a Changelog Entry](#adding-a-changelog-entry)

## Contributing 101

### Building and running `consul-k8s-control-plane`

To build and install the control plane binary `consul-k8s-control-plane` locally, Go version 1.17.0+ is required.
You will also need to install the Docker engine:

- [Docker for Mac](https://docs.docker.com/engine/installation/mac/)
- [Docker for Windows](https://docs.docker.com/engine/installation/windows/)
- [Docker for Linux](https://docs.docker.com/engine/installation/linux/ubuntulinux/)

Install [gox](https://github.com/mitchellh/gox) (v1.14+). For Mac and Linux:
  ```bash
  brew install gox
  ```

Clone the repository:

```shell
git clone https://github.com/hashicorp/consul-k8s.git
```

Compile the `consul-k8s-control-plane` binary for your local machine:

```shell
make control-plane-dev
```

This will compile the `consul-k8s-control-plane` binary into `control-plane/bin/consul-k8s-control-plane` as
well as your `$GOPATH` and run the test suite.

Run the tests:

```shell
make control-plane-test
```

Run a specific test in the suite. Change directory into `control-plane`.

```shell
go test ./... -run SomeTestFunction_name
```

To create a docker image with your local changes:

```shell
make control-plane-dev-docker
```

To use your Docker image in a dev deployment of Consul K8s, push the image to Docker Hub or your own registry. Deploying from local images is not supported.

```
docker tag consul-k8s-control-plane-dev <DOCKER-HUB-USERNAME>/consul-k8s-control-plane-dev
docker push <DOCKER-HUB-USERNAME>/consul-k8s-control-plane-dev
```

Create a `values.dev.yaml` file that includes the `global.imageK8S` flag to point to dev images you just pushed:

```yaml
global:
  tls:
    enabled: true
  imageK8S: <DOCKER-HUB-USERNAME>/consul-k8s-control-plane-dev
server:
  replicas: 1
connectInject:
  enabled: true
ui:
  enabled: true
  service:
    enabled: true
controller:
  enabled: true
```

Run a `helm install` from the project root directory to target your dev version of the Helm chart.

```shell
helm install consul --create-namespace -n consul -f ./values.dev.yaml ./charts/consul
```

### Building and running the `consul-k8s` CLI

Compile the `consul-k8s` CLI binary for your local machine:

```shell
make cli-dev
```
This will compile the `consul-k8s` binary into `cli/bin/consul-k8s` as well as your `$GOPATH`.

Run the CLI as follows:

```shell
consul-k8s version
```

### Making changes to consul-k8s

The first step to making changes is to fork Consul K8s. Afterwards, the easiest way
to work on the fork is to set it as a remote of the Consul K8s project:

1. Rename the existing remote's name: `git remote rename origin upstream`.
1. Add your fork as a remote by running
   `git remote add origin <github url of fork>`. For example:
   `git remote add origin https://github.com/myusername/consul-k8s`.
1. Checkout a feature branch: `git checkout -t -b new-feature`
1. Make changes (i.e. `git commit -am 'message'`)
1. Push changes to the fork when ready to submit PR:
   `git push -u origin new-feature`

>Note: If you make any changes to the code, run `gofmt -s -w` to automatically format the code according to Go standards.

### Running linters locally
[`golangci-lint`](https://golangci-lint.run/) is used in CI to enforce coding and style standards and help catch bugs ahead of time.
The configuration that CI runs is stored in `.golangci.yml` at the top level of the repository.
Please ensure your code passes by running `golangci-lint run` at the top level of the repository and addressing
any issues prior to submitting a PR.

Version 1.41.1 or higher of [`golangci-lint`](https://github.com/golangci/golangci-lint/releases/tag/v1.41.1) is currently required.

### Rebasing contributions against main

PRs in this repo are merged using the [`rebase`](https://git-scm.com/docs/git-rebase) method. This keeps
the git history clean by adding the PR commits to the most recent end of the commit history. It also has
the benefit of keeping all the relevant commits for a given PR together, rather than spread throughout the
git history based on when the commits were first created.

If the changes in your PR do not conflict with any of the existing code in the project, then Github supports
automatic rebasing when the PR is accepted into the code. However, if there are conflicts (there will be
a warning on the PR that reads "This branch cannot be rebased due to conflicts"), you will need to manually
rebase the branch on main, fixing any conflicts along the way before the code can be merged.

---

## Creating a new CRD

### The Structs
1. Run the generate command from the `control-plane` directory: (installation instructions for `operator-sdk` found [here](https://sdk.operatorframework.io/docs/installation/):
    ```bash
    operator-sdk create api --group consul --version v1alpha1 --kind IngressGateway --controller --namespaced=true --make=false --resource=true
    ```
1. Re-order the generated ingressgateway_types.go file, so it looks like:
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
    // +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
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
1. Go to the Consul `api` package for the config entry, e.g. https://github.com/hashicorp/consul/blob/main/api/config_entry_gateways.go
1. Copy the top-level fields over into the `Spec` struct except for
   `Kind`, `Name`, `Namespace`, `Partition`, `Meta`, `CreateIndex` and `ModifyIndex`. In this
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
1. Delete the file `control-plane/controllers/suite_test.go`. We don't write suite tests, just unit tests.
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
1. Copy an existing webhook to `control-plane/api/v1alpha/ingressgateway_webhook.go`
1. Replace the names
1. Ensure you've correctly replaced the names in the kubebuilder annotation, ensure the plurality is correct
    ```go
      // +kubebuilder:webhook:verbs=create;update,path=/mutate-v1alpha1-ingressgateway,mutating=true,failurePolicy=fail,groups=consul.hashicorp.com,resources=ingressgateways,versions=v1alpha1,name=mutate-ingressgateway.consul.hashicorp.com,sideEffects=None,admissionReviewVersions=v1beta1;v1
    ```
1. Ensure you update the path to match the annotation in the `SetupWithManager` method:
    ```go
    func (v *IngressGatewayWebhook) SetupWithManager(mgr ctrl.Manager) {
      v.decoder = admission.NewDecoder(mgr.GetScheme())
      mgr.GetWebhookServer().Register("/mutate-v1alpha1-ingressgateway", &admission.Webhook{Handler: v})
}
    ```

### Update command.go
1. Add your resource name to `control-plane/api/common/common.go`:
    ```go
    const (
        ...
        IngressGateway    string = "ingressgateway"
    ```
1. Update `control-plane/subcommand/inject-connect/command.go` and add your controller:
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
1. Update `control-plane/subcommand/inject-connect/command.go` and add your webhook
    ```go
    (&v1alpha1.IngressGatewayWebhook
      Client:     mgr.GetClient(),
      Logger:     ctrl.Log.WithName("webhooks").WithName(common.IngressGateway),
      ConsulMeta: consulMeta,
    }).SetupWithManager(mgr)
    ```

### Generating YAML
1. Run `make ctrl-manifests` to generate the CRD and webhook YAML.
1. Uncomment your CRD in `control-plane/config/crd/kustomization` under `patches:`
1. Update the sample, e.g. `control-plane/config/samples/consul_v1alpha1_ingressgateway.yaml` to a valid resource
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

### Updating Helm chart
1. Update `charts/consul/templates/connect-inject-mutatingwebhookconfiguration` with the webhook for this resource
   using the updated `control-plane/config/webhook/manifests.v1beta1.yaml` and replacing `clientConfig.service.name/namespace`
   with the templated strings shown below to match the other webhooks.:
    ```yaml
    - clientConfig:
        service:
          name: {{ template "consul.fullname" . }}-connect-injector
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
1. Update `charts/consul/templates/connect-inject-clusterrole.yaml` to allow the controller to
   manage your resource type.

### Testing A New CRD
1. Build a Docker image for consul-k8s via `make control-plane-dev-docker` and push to a docker repository:
    ```
    docker tag consul-k8s-control-plane-dev <DOCKER-HUB-USERNAME>/consul-k8s-control-plane-dev:<version>
    docker push <DOCKER-HUB-USERNAME>/consul-k8s-control-plane-dev:<version>
    ```
1. Install using the updated Helm repository, with a values like:
    ```yaml
    global:
      imageK8S: lkysow/consul-k8s-control-plane-dev:nov26
      name: consul
    server:
      replicas: 1
      bootstrapExpect: 1
    ui:
      enabled: true
    connectInject:
      enabled: true
    ```
1. Create a sample CRD
1. Run `kubectl apply -f <path-to-crd>` to apply your sample CRD.
1. Check its synced status (for example CRD called ingressgateway):
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

### Update consul-k8s Acceptance Tests
1. Add a test resource to `acceptance/tests/fixtures/crds/ingressgateway.yaml`. Ideally it requires
   no other resources. For example, I used a `tcp` service so it didn't require a `ServiceDefaults`
   resource to set its protocol to something else.
1. Update `acceptance/tests/controller/controller_test.go` and `acceptance/tests/controller/controller_namespaces_test.go`.
1. Test locally, then submit a PR that uses your Docker image as `global.imageK8S`.

---

## Adding a new ACL Token

Checklist for getting server-acl-init to generate a new ACL token. The examples in this checklist use
a token named `foo`.

### Control Plane

* `control-plane/subcommand/server-acl-init/command.go`
    * Add `flagCreateFooToken bool` to vars list
    * Initialize flag in `init`

      ```go
      c.flags.BoolVar(&c.flagCreateFooToken, "create-foo-token", false,
        "<docs for flag>")
      ```
    * Add `if` statement in `Run` to create your token (follow placement of other tokens).
      You'll need to decide if you need a local token (use `createLocalACL()`) or a global token (use `createGlobalACL()`).

      ```go
      if c.flagCreateFooToken {
          err := c.createLocalACL("foo", fooRules, consulDC, isPrimary, consulClient)
          if err != nil {
              c.log.Error(err.Error())
              return 1
          }
      }
      ```
* `control-plane/subcommand/server-acl-init/rules.go`
    * Add a function that outputs your rules using a template
      (if the rules don't need to be templated just use a `const string`):
      ```go
      func (c *Command) fooRules() (string, error) {
      ```
* `control-plane/subcommand/server-acl-init/rules_test.go`
    * Add test following the pattern of other tests (`TestFooRules`)
* `control-plane/subcommand/server-acl-init/command_test.go`
    * Add test cases using your flag to the following tests:
        * `TestRun_TokensPrimaryDC`
        * `TestRun_TokensReplicatedDC`
        * `TestRun_TokensWithProvidedBootstrapToken`

### Helm

* `charts/consul/templates/server-acl-init-job.yaml`
    * Add conditional to set your flag:

      ```yaml
      {{- if .Values.foo.enabled }}
      -create-foo-token=true \
      {{- end }}
* `charts/consul/test/unit/server-acl-init-job.bats`
    * Test the conditional:

      ```bash
      #--------------------------------------------------------------------
      # foo

      @test "serverACLInit/Job: -create-foo-token not set by default" {
        cd `chart_dir`
        local actual=$(helm template \
            -s templates/server-acl-init-job.yaml  \
            --set 'global.acls.manageSystemACLs=true' \
            . | tee /dev/stderr |
            yq '.spec.template.spec.containers[0].command | any(contains("create-foo-token"))' | tee /dev/stderr)
        [ "${actual}" = "false" ]
      }

      @test "serverACLInit/Job: -create-foo-token set when foo.enabled=true" {
        cd `chart_dir`
        local actual=$(helm template \
            -s templates/server-acl-init-job.yaml  \
            --set 'global.acls.manageSystemACLs=true' \
            --set 'foo.enabled=true' \
            . | tee /dev/stderr |
            yq '.spec.template.spec.containers[0].command | any(contains("create-foo-token"))' | tee /dev/stderr)
        [ "${actual}" = "true" ]
      }
      ```




## Testing the Helm Chart
The Helm chart ships with both unit and acceptance tests.

The unit tests don't require any active Kubernetes cluster and complete
very quickly. These should be used for fast feedback during development.
The acceptance tests require a Kubernetes cluster with a configured `kubectl`.

### Prerequisites
* [Bats](https://github.com/bats-core/bats-core) (Currently, must use v1.6.0+.)
  ```bash
  brew install bats-core
  ```
* [yq](https://pypi.org/project/yq/)
  ```bash
  brew install python-yq
  ```
* [Helm 3](https://helm.sh) (Currently, must use v3.8.0+.)
  ```bash
  brew install kubernetes-helm
  ```
* [go](https://golang.org/) (v1.14+)
  ```bash
  brew install golang
  ```

### Running The Tests

#### Unit Tests
To run all the unit tests:

    bats ./charts/consul/test/unit

To run tests in a specific file:

    bats ./charts/consul/test/unit/<filename>.bats

To run tests in parallel use the `--jobs` flag (requires parallel `brew install parallel`):

    bats ./charts/consul/test/unit/<filename>.bats --jobs 8

To run a specific test by name use the `--filter` flag:

    bats ./charts/consul/test/unit/<filename>.bats --filter "my test name"

#### Acceptance Tests
##### Pre-requisites
* [gox](https://github.com/mitchellh/gox) (v1.14+)
  ```bash
  brew install gox
  ```
  ```bash
  make cli-dev
  ```
To run the acceptance tests:

    cd acceptance/tests
    go test ./... -p 1

The above command will run all tests that can run against a single Kubernetes cluster,
using the current context set in your kubeconfig locally.

**Note:** You must run all tests in serial by passing the `-p 1` flag
because the test suite currently does not support parallel execution.

You can run other tests by enabling them by passing appropriate flags to `go test`.
For example, to run mesh gateway tests, which require two Kubernetes clusters,
you may use the following command:

    go test ./... -p 1 -timeout 20m \
        -enable-multi-cluster \
        -kube-contexts="<name of the primary Kubernetes context>,<name of the secondary Kubernetes context>, etc.>"

Below is the list of available flags:

```
-consul-image string
    The Consul image to use for all tests.
-consul-k8s-image string
    The consul-k8s image to use for all tests.
-debug-directory
    The directory where to write debug information about failed test runs, such as logs and pod definitions. If not provided, a temporary directory will be created by the tests.
-enable-enterprise
    If true, the test suite will run tests for enterprise features. Note that some features may require setting the enterprise license flag below or the env var CONSUL_ENT_LICENSE.
-enable-multi-cluster
    If true, the tests that require multiple Kubernetes clusters will be run. At least one of -secondary-kubeconfig or -secondary-kubecontext is required when this flag is used.
-enable-openshift
    If true, the tests will automatically add Openshift Helm value for each Helm install.
-enable-pod-security-policies
    If true, the test suite will run tests with pod security policies enabled.
-enable-transparent-proxy
    If true, the test suite will run tests with transparent proxy enabled.
    This applies only to tests that enable connectInject.
-enterprise-license
    The enterprise license for Consul.
-kubeconfigs string
    The comma separated list of Kubernetes configs to use (eg. "~/.kube/config,~/.kube/config2"). The first in the list will be treated as the primary config, followed by the secondary, etc. If the list is empty, or items are blank, then the default kubeconfig path (~/.kube/config) will be used.
-kube-contexts string
    The comma separated list of Kubernetes contexts to use (eg. "kind-dc1,kind-dc2"). The first in the list will be treated as the primary context, followed by the secondary, etc. If the list is empty, or items are blank, then the current context will be used.
-kube-namespaces string
    The comma separated list of Kubernetes namespaces to use (eg. "consul,consul-secondary"). The first in the list will be treated as the primary namespace, followed by the secondary, etc. If the list is empty, or fields are blank, then the current namespace will be used.
-no-cleanup-on-failure
    If true, the tests will not cleanup Kubernetes resources they create when they finish running.Note this flag must be run with -failfast flag, otherwise subsequent tests will fail.
```

**Note:** There is a Terraform configuration in the
[`charts/consul/test/terraform/gke`](./test/terraform/gke) directory
that can be used to quickly bring up a GKE cluster and configure
`kubectl` and `helm` locally. This can be used to quickly spin up a test
cluster for acceptance tests. Unit tests _do not_ require a running Kubernetes
cluster.

### Writing Unit Tests

Changes to the Helm chart should be accompanied by appropriate unit tests.

#### Formatting

- Put tests in the test file in the same order as the variables appear in the `values.yaml`.
- Start tests for a chart value with a header that says what is being tested, like this:
    ```
    #--------------------------------------------------------------------
    # annotations
    ```

- Name the test based on what it's testing in the following format (this will be its first line):
    ```
    @test "<section being tested>: <short description of the test case>" {
    ```

    When adding tests to an existing file, the first section will be the same as the other tests in the file.

#### Test Details

[Bats](https://github.com/bats-core/bats-core) provides a way to run commands in a shell and inspect the output in an automated way.
In all of the tests in this repo, the base command being run is [helm template](https://docs.helm.sh/helm/#helm-template) which turns the templated files into straight yaml output.
In this way, we're able to test that the various conditionals in the templates render as we would expect.

Each test defines the files that should be rendered using the `-x` flag, then it might adjust chart values by adding `--set` flags as well.
The output from this `helm template` command is then piped to [yq](https://pypi.org/project/yq/).
`yq` allows us to pull out just the information we're interested in, either by referencing its position in the yaml file directly or giving information about it (like its length).
The `-r` flag can be used with `yq` to return a raw string instead of a quoted one which is especially useful when looking for an exact match.

The test passes or fails based on the conditional at the end that is in square brackets, which is a comparison of our expected value and the output of  `helm template` piped to `yq`.

The `| tee /dev/stderr ` pieces direct any terminal output of the `helm template` and `yq` commands to stderr so that it doesn't interfere with `bats`.

#### Test Examples

Here are some examples of common test patterns:

- Check that a value is disabled by default

    ```
    @test "ui/Service: no type by default" {
      cd `chart_dir`
      local actual=$(helm template \
          -s templates/ui-service.yaml  \
          . | tee /dev/stderr |
          yq -r '.spec.type' | tee /dev/stderr)
      [ "${actual}" = "null" ]
    }
    ```

    In this example, nothing is changed from the default templates (no `--set` flags), then we use `yq` to retrieve the value we're checking, `.spec.type`.
    This output is then compared against our expected value (`null` in this case) in the assertion `[ "${actual}" = "null" ]`.


- Check that a template value is rendered to a specific value
    ```
    @test "ui/Service: specified type" {
      cd `chart_dir`
      local actual=$(helm template \
          -s templates/ui-service.yaml  \
          --set 'ui.service.type=LoadBalancer' \
          . | tee /dev/stderr |
          yq -r '.spec.type' | tee /dev/stderr)
      [ "${actual}" = "LoadBalancer" ]
    }
    ```

    This is very similar to the last example, except we've changed a default value with the `--set` flag and correspondingly changed the expected value.

- Check that a template value contains several values
    ```
    @test "syncCatalog/Deployment: to-k8s only" {
      cd `chart_dir`
      local actual=$(helm template \
          -s templates/sync-catalog-deployment.yaml  \
          --set 'syncCatalog.enabled=true' \
          --set 'syncCatalog.toConsul=false' \
          . | tee /dev/stderr |
          yq '.spec.template.spec.containers[0].command | any(contains("-to-consul=false"))' | tee /dev/stderr)
      [ "${actual}" = "true" ]

      local actual=$(helm template \
          -s templates/sync-catalog-deployment.yaml  \
          --set 'syncCatalog.enabled=true' \
          --set 'syncCatalog.toConsul=false' \
          . | tee /dev/stderr |
          yq '.spec.template.spec.containers[0].command | any(contains("-to-k8s"))' | tee /dev/stderr)
      [ "${actual}" = "false" ]
    }
    ```
    In this case, the same command is run twice in the same test.
    This can be used to look for several things in the same field, or to check that something is not present that shouldn't be.

    *Note:* If testing more than two conditions, it would be good to separate the `helm template` part of the command from the `yq` sections to reduce redundant work.

- Check that an entire template file is not rendered
    ```
    @test "syncCatalog/Deployment: disabled by default" {
      cd `chart_dir`
      assert_empty helm template \
          -s templates/sync-catalog-deployment.yaml \
          .
    }
    ```
    Here we are using the `assert_empty` helper command.

### Writing Acceptance Tests

If you are adding a feature that fits thematically with one of the existing test suites,
then you need to add your test cases to the existing test files.
Otherwise, you will need to create a new test suite.

We recommend to start by either copying the [example test](acceptance/tests/example/example_test.go)
or the whole [example test suite](acceptance/tests/example),
depending on the test you need to add.

#### Adding Test Suites

To add a test suite, copy the [example test suite](acceptance/tests/example)
and uncomment the code you need in the [`main_test.go`](acceptance/tests/example/main_test.go) file.

At a minimum, this file needs to contain the following:

```go
package example

import (
	"os"
	"testing"

	"github.com/hashicorp/consul-helm/test/acceptance/framework"
)

var suite framework.Suite

func TestMain(m *testing.M) {
	suite = framework.NewSuite(m)
	os.Exit(suite.Run())
}
```

If the test suite needs to run only when certain test flags are passed,
you need to handle that in the `TestMain` function.

```go
func TestMain(m *testing.M) {
    // First, create a new suite so that all flags are parsed.
    suite = framework.NewSuite(m)

    // Run the suite only if our example feature test flag is set.
    if suite.Config().EnableExampleFeature {
        os.Exit(suite.Run())
    } else {
        fmt.Println("Skipping example feature tests because -enable-example-feature is not set")
        os.Exit(0)
    }
}
```

#### Example Test

We recommend using the [example test](acceptance/tests/example/example_test.go)
as a starting point for adding your tests.

To write a test, you need access to the environment and context to run it against.
Each test belongs to a test **suite** that contains a test **environment** and test **configuration** created from flags passed to `go test`.
A test **environment** contains references to one or more test **contexts**,
which represents one Kubernetes cluster.

```go
func TestExample(t *testing.T) {
  // Get test configuration.
  cfg := suite.Config()

  // Get the default context.
  ctx := suite.Environment().DefaultContext(t)

  // Create Helm values for the Helm install.
  helmValues := map[string]string{
      "exampleFeature.enabled": "true",
  }

  // Generate a random name for this test.
  releaseName := helpers.RandomName()

  // Create a new Consul cluster object.
  consulCluster := framework.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

  // Create the Consul cluster with Helm.
  consulCluster.Create(t)

  // Make test assertions.
}
```

Please see [wan federation tests](acceptance/tests/wan-federation/wan_federation_test.go)
for an example of how to use write a test that uses multiple contexts.

#### Writing Assertions

Depending on the test you're writing, you may need to write assertions
either by running `kubectl` commands, calling the Kubernetes API, or
the Consul API.

To run `kubectl` commands, you need to get `KubectlOptions` from the test context.
There are a number of `kubectl` commands available in the `helpers/kubectl.go` file.
For example, to call `kubectl apply` from the test write the following:

```go
helpers.KubectlApply(t, ctx.KubectlOptions(t), filepath)
```

Similarly, you can obtain Kubernetes client from your test context.
You can use it to, for example, read all services in a namespace:

```go
k8sClient := ctx.KubernetesClient(t)
services, err := k8sClient.CoreV1().Services(ctx.KubectlOptions(t).Namespace).List(metav1.ListOptions{})
```

To make Consul API calls, you can get the Consul client from the `consulCluster` object,
indicating whether the client needs to be secure or not (i.e. whether TLS and ACLs are enabled on the Consul cluster):

```go
consulClient := consulCluster.SetupConsulClient(t, true)
consulServices, _, err := consulClient.Catalog().Services(nil)
```

#### Cleaning Up Resources

Because you may be creating resources that will not be destroyed automatically
when a test finishes, you need to make sure to clean them up. Most methods and objects
provided by the framework already do that, so you don't need to worry cleaning them up.
However, if your tests create Kubernetes objects, you need to clean them up yourself by
calling `helpers.Cleanup` function.

**Note:** If you want to keep resources after a test run for debugging purposes,
you can run tests with `-no-cleanup-on-failure` flag.
You need to make sure to clean them up manually before running tests again.

#### When to Add Acceptance Tests

Sometimes adding an acceptance test for the feature you're writing may not be the right thing.
Here are some things to consider before adding a test:

* Is this a test for a happy case scenario?
  Generally, we expect acceptance tests to test happy case scenarios. If your test does not,
  then perhaps it could be tested by either a unit test in this repository or a test in the
  [consul-k8s](https://github.com/hashicorp/consul-k8s) repository.
* Is the test you're going to write for a feature that is scoped to one of the underlying componenets of this Helm chart,
  either Consul itself or consul-k8s? In that case, it should be tested there rather than in the Helm chart.
  For example, we don't expect acceptance tests to include all the permutations of the consul-k8s commands
  and their respective flags. Something like that should be tested in the consul-k8s repository.

---

## Using Acceptance Test Framework to Debug
### Acceptance Tests

The [consul-k8s](https://github.com/hashicorp/consul-k8s) repository has an extensive list of [acceptance](https://github.com/hashicorp/consul-k8s/tree/main/acceptance/tests)
tests that are used by CI to run per-PR and nightly acceptance tests.
It is built on its own framework that uses Helm and the consul-k8s CLI to deploy consul (and other tools) in various
configurations that provide test coverage for most features that exist and provides coverage for more advanced deployments
than are typically covered in guides.
Importantly, it is **automated**, so you are able to rapidly deploy known working
configurations in known working environments.
It can be very helpful for bootstrapping complex environments such as when using Vault as a CA for Consul or for federating test clusters.

The tests are organized like this :
```shell
demo $ tree -L 1 -d acceptance/tests
acceptance/tests
├── api-gateway
├── basic
├── cli
├── cloud
├── config-entries
├── connect
├── consul-dns
├── datadog
├── example
├── fixtures
├── ingress-gateway
├── metrics
├── partitions
├── peering
├── sameness
├── segments
├── server
├── snapshot-agent
├── sync
├── terminating-gateway
├── vault
└── wan-federation
```

### Basic Running of Tests
Any given test can be run either through GoLand or another IDE, or via command line using `go test -run`.

To run all of the connect tests from command line:
```shell
$ cd acceptance/tests
$ go test ./connect/... -v -p 1 -timeout 2h -failfast -use-kind -no-cleanup-on-failure -kubecontext=kind-dc1 -secondary-kubecontext=kind-dc2 -enable-enterprise -enable-multi-cluster -debug-directory=/tmp/debug -consul-k8s-image=kyleschochenmaier/consul-k8s-acls
```

When running from command line a few things are important:
* Some tests use Enterprise features, in which case you need:
    * Set environment variables `CONSUL_ENT_LICENSE` and possibly `VAULT_LICENSE`.
    * Use `-enable-enterprise` on command line when running the test.
* Multi-cluster tests require `-enable-multi-cluster -kubecontext=kind-dc1 -secondary-kubecontext=kind-dc2`
* Using `./<test-directory>/...` is required as part of the command-line to pick up necessary environmental config.

### Using the framework to debug in an environment
=> NOTE: It is helpful to tune the docker desktop resource settings so that docker has at least 4GB memory, plenty of cpu cores and 2GB of swap.

* If using Kind, `-use-kind` should be added, and be sure you cluster is up and running:
```shell
$ kind create cluster --name=dc1 && kind create cluster --name=dc2
```
* Pick a test which replicates the environment you are wanting to work with.
  Ex: pick a test from `partitions/` or `vault/` or `connect/`.
* If you need the environment to persist, add a `time.Sleep(1*time.Hour)` to the end of the test in the test file.
* Use the following flags if you need to use or test out a specific consul/k8s image:
  `-consul-k8s-image=<your-custom-image>` && `-consul-image=<your-custom-image>`
* You can set custom helm flags by modifying the test file directly in the respective directory.

Finally, you have two options on how you can run your test:
1. Take the following steps, this will run the test through to completion but not teardown any resources created by the test so you can inspect the state of the cluster
at that point. You will be responsible for cleaning up the resources or deleting the cluster entirely when you're done.
```shell
$ cd acceptance/tests
$ go test -run Vault_WANFederationViaGateways ./vault/... -p 1 -timeout 2h -failfast -use-kind -no-cleanup-on-failure -kubecontext=kind-dc1 -secondary-kubecontext=kind-dc2 -enable-multi-cluster -debug-directory=/tmp/debug
```
You can interact with the running kubernetes clusters now using `kubectl [COMMAND] --context=<kind-dc1/kind-dc2>`

* `kind delete clusters --all` is helpful for cleanup!

2. The other option is to use the helper method in the framework: `helpers.WaitForInput(t)` at the spot in your acceptance test where you would like to pause execution to inspect the cluster. This will pause the test execution until you execute a request to `localhost:8715` which tells the test to continue running, you can override the port value used by setting the `CONSUL_K8S_TEST_PAUSE_PORT` environment variable to a port of your choosing. When running the tests with the `-v` flag you will see a log output of the endpoint that the test is waiting on.

```go
import "github.com/hashicorp/consul-k8s/acceptance/framework/helpers"

func TestSomeTest(t *testing.T) {
  // stuff to setup

  // test execution will pause here until the endpoint is hit
  helpers.WaitForInput(t)

  // rest of test
}
```

### Example Debugging session using the acceptance test framework to bootstrap and debug a Vault backed federated Consul installation:
This test utilizes the `consul-k8s` acceptance test framework, with a custom consul-k8s branch which:
* Modifies the acceptance test to use custom consul+consul-k8s images and sleeps at the end of the test to allow analysis.
* Modifies the helm chart to pass in `connect_ca.intermediate_cert_ttl` and `connect_ca.leaf_cert_ttl` in the `server-configmap`

1. First clone the consul-k8s repo and then check out the branch locally: `git checkout origin/consul-vault-provider-wanfed-acceptance`.
2. Start the kind clusters: `kind create cluster --name=dc1 && kind create cluster --name=dc2`
3. run the `TestVault_WANFederationViaGateways` acceptance test in `acceptance/tests/vault/vault_wan_fed_test.go` - I use goland, but this command should get you most of the way:
```shell
$ cd acceptance/tests
$ go test -run Vault_WANFederationViaGateways ./vault/... -p 1 -timeout 2h -failfast -use-kind -no-cleanup-on-failure -kubecontext=kind-dc1 -secondary-kubecontext=kind-dc2 -enable-multi-cluster -debug-directory=/tmp/debug
```
NOTE: This specific acceptance test is considered FLAKY with Kind, if things don't come up it's best to run against GKE/AKS/etc, in which case you just modify the `kubecontext` command parameters to point to your clusters. It is worth noting that you will need to setup any necessary networking for non-Kind clusters manually.

NOTE: This test requires a VAULT_LICENSE set as an environment variable in the shell where you run `go test`

4. Wait 10-20 minutes to allow the first intermediate ca renewal, this test is particularly resource intensive so it can take time for everything to come online on a laptop, use `kubectl get pods` to validate that `static-server` and `static-client` have been deployed and are online.

You can validate the ICA rotation by doing:
```shell
# Fetch the vault root token:
$ kubectl get secrets <vault-release-name>-root-token -o json //----> b64 decode the `data.token` field.
$ kubectl exec -it <vault-server> -- sh
$ export VAULT_TOKEN=<from above>
$ export VAULT_ADDR=https://<vault-release-name>-vault:8200

# Fetch the consul bootstrap token
$ vault kv get consul/secret/bootstrap

# Examine the vault issuers, there should be 2 by now if ICA renewal has occured:
# NOTE: for a federated setup the issuers url for dc2 is `vault list dc2/connect_inter/issuers`!
$ vault list dc1/connect_inter/issuers

Keys
----
29bdffbd-87ec-cfe0-fd05-b78f99eba243
344eea3c-f085-943a-c3ff-66721ef408f4

# Now login to the consul-server
$ kubectl exec -it <consul-server> -- sh
$ export CONSUL_HTTP_TOKEN=<bootstrap token from above>
$ export CONSUL_HTTP_ADDR=https://localhost:8501
$ export CONSUL_HTTP_SSL_VERIFY=false

# Read the `connect/ca/roots` endpoint:
# It should change + rotate with the expiration of the ICA (defined by `intermediate_cert_ttl` which is `15m` in the branch for this gist.

$ curl -k --header "X-Consul-Token: 1428da53-5e88-db1a-6ad5-e50212b011da" https://127.0.0.1:8501/v1/agent/connect/ca/roots | jq
 .
  % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
                                 Dload  Upload   Total   Spent    Left  Speed
100  3113  100  3113    0     0   6222      0 --:--:-- --:--:-- --:--:--  7705
{
  "ActiveRootID": "36:be:19:0e:56:d1:c2:1a:d8:54:22:97:88:3c:91:17:1d:d2:d3:e0",
  "TrustDomain": "34a76791-b9b2-b93e-b0e4-1989ed11a28e.consul",
  "Roots": [
    {
      "ID": "36:be:19:0e:56:d1:c2:1a:d8:54:22:97:88:3c:91:17:1d:d2:d3:e0",
      "Name": "Vault CA Primary Cert",
      "SerialNumber": 15998414315735550000,
      "SigningKeyID": "fe:b9:d6:0b:c6:ce:2c:25:4f:d8:59:cb:11:ea:a5:42:5f:8e:41:4b",
      "ExternalTrustDomain": "34a76791-b9b2-b93e-b0e4-1989ed11a28e",
      "NotBefore": "2022-11-16T20:16:15Z",
      "NotAfter": "2032-11-13T20:16:45Z",
      "RootCert": "-----BEGIN CERTIFICATE-----\nMIICLDCCAdKgAwIBAgIUKQ9BPHF9mtC7yFPC3gXJDpLxCHIwCgYIKoZIzj0EAwIw\nLzEtMCsGA1UEAxMkcHJpLTEwOTJudTEudmF1bHQuY2EuMzRhNzY3OTEuY29uc3Vs\nMB4XDTIyMTExNjIwMTYxNVoXDTMyMTExMzIwMTY0NVowLzEtMCsGA1UEAxMkcHJp\nLTEwOTJudTEudmF1bHQuY2EuMzRhNzY3OTEuY29uc3VsMFkwEwYHKoZIzj0CAQYI\nKoZIzj0DAQcDQgAETnpGixC1kW8ep2JcGjRR2jbdESvjlEm9nSIWVAcilemUGFwi\nJ0YW0XUmJeEzRyfwLXnOw6voPzXRf1zXKjdTD6OByzCByDAOBgNVHQ8BAf8EBAMC\nAQYwDwYDVR0TAQH/BAUwAwEB/zAdBgNVHQ4EFgQUtb6EjDxyI+myIjDc+7KbiN8u\n8XowHwYDVR0jBBgwFoAUtb6EjDxyI+myIjDc+7KbiN8u8XowZQYDVR0RBF4wXIIk\ncHJpLTEwOTJudTEudmF1bHQuY2EuMzRhNzY3OTEuY29uc3VshjRzcGlmZmU6Ly8z\nNGE3Njc5MS1iOWIyLWI5M2UtYjBlNC0xOTg5ZWQxMWEyOGUuY29uc3VsMAoGCCqG\nSM49BAMCA0gAMEUCIHBezFSQAK5Nolf0rs3ErvlDcA8Z9esldh6gHupuGsNkAiEA\n9qL+P9PJAW4CrbTL0iF2yZUyJC2nwSSa2K0nYG8bXWQ=\n-----END CERTIFICATE-----\n",
      "IntermediateCerts": [
        "-----BEGIN CERTIFICATE-----\nMIICLzCCAdSgAwIBAgIUbILCP3ODM4ScNBOm0jw59Fxju0swCgYIKoZIzj0EAwIw\nLzEtMCsGA1UEAxMkcHJpLTEwOTJudTEudmF1bHQuY2EuMzRhNzY3OTEuY29uc3Vs\nMB4XDTIyMTExNjIwMzIxNloXDTIyMTExNjIwNDc0NlowMDEuMCwGA1UEAxMlcHJp\nLTE4MThxNWlnLnZhdWx0LmNhLjM0YTc2NzkxLmNvbnN1bDBZMBMGByqGSM49AgEG\nCCqGSM49AwEHA0IABI30ikgrwTjbPaGgfNYkushvrEUUpxLzxMMEBlE82ilog1RW\nqwuEU29Qsa+N4SrfOf37xNv/Ey8SXPs5l2HmXJWjgcwwgckwDgYDVR0PAQH/BAQD\nAgEGMA8GA1UdEwEB/wQFMAMBAf8wHQYDVR0OBBYEFCZpC/BTdaggL2kj6Dfyk3+a\nNqBvMB8GA1UdIwQYMBaAFLW+hIw8ciPpsiIw3Puym4jfLvF6MGYGA1UdEQRfMF2C\nJXByaS0xODE4cTVpZy52YXVsdC5jYS4zNGE3Njc5MS5jb25zdWyGNHNwaWZmZTov\nLzM0YTc2NzkxLWI5YjItYjkzZS1iMGU0LTE5ODllZDExYTI4ZS5jb25zdWwwCgYI\nKoZIzj0EAwIDSQAwRgIhAJ8RHgR5qkyW2q866vGYJy+7BJ4zUXs3OJ76QLmxxU3K\nAiEA70S7wBEm1ZduTAk1ZfZPJEUGxvAXAcgy7EWeO/6MJ5o=\n-----END CERTIFICATE-----\n",
        "-----BEGIN CERTIFICATE-----\nMIICLTCCAdKgAwIBAgIUU3qwESuhh4PgW3/tnHDn3qnBMrAwCgYIKoZIzj0EAwIw\nLzEtMCsGA1UEAxMkcHJpLTEwOTJudTEudmF1bHQuY2EuMzRhNzY3OTEuY29uc3Vs\nMB4XDTIyMTExNjIwNDAxNloXDTIyMTExNjIwNTU0NlowLzEtMCsGA1UEAxMkcHJp\nLTFkY2hkbGkudmF1bHQuY2EuMzRhNzY3OTEuY29uc3VsMFkwEwYHKoZIzj0CAQYI\nKoZIzj0DAQcDQgAEpj0BWPkcH82su9XGOo9rN5Zr5+Jyp68LiHy+qlIgH3L+OAir\nYgmXmJfuNwI8S2BB8cu0Gk3w5cTF7O0p/qAghaOByzCByDAOBgNVHQ8BAf8EBAMC\nAQYwDwYDVR0TAQH/BAUwAwEB/zAdBgNVHQ4EFgQU/rnWC8bOLCVP2FnLEeqlQl+O\nQUswHwYDVR0jBBgwFoAUtb6EjDxyI+myIjDc+7KbiN8u8XowZQYDVR0RBF4wXIIk\ncHJpLTFkY2hkbGkudmF1bHQuY2EuMzRhNzY3OTEuY29uc3VshjRzcGlmZmU6Ly8z\nNGE3Njc5MS1iOWIyLWI5M2UtYjBlNC0xOTg5ZWQxMWEyOGUuY29uc3VsMAoGCCqG\nSM49BAMCA0kAMEYCIQCtq4LiZzkiIKUES9MrzUEflg7wcwQf7Km+8RcOGQbz9QIh\nANWHWt1fe8Hl1wQ55qxsV5lSfOpGAox5WHpgnsBC7cwU\n-----END CERTIFICATE-----\n"
      ],
      "Active": true,
      "PrivateKeyType": "ec",
      "PrivateKeyBits": 256,
      "CreateIndex": 11,
      "ModifyIndex": 797
    }
  ]
}

# You can x509 decode the ICA certs to verify they have been updated and have correct expiry:
$ openssl x509 -in cert.crt -text -noout
Certificate:
    Data:
        Version: 3 (0x2)
        Serial Number:
            53:7a:b0:11:2b:a1:87:83:e0:5b:7f:ed:9c:70:e7:de:a9:c1:32:b0
    Signature Algorithm: ecdsa-with-SHA256
        Issuer: CN=pri-1092nu1.vault.ca.34a76791.consul
        Validity
            Not Before: Nov 16 20:40:16 2022 GMT
            Not After : Nov 16 20:55:46 2022 GMT
        Subject: CN=pri-1dchdli.vault.ca.34a76791.consul
        Subject Public Key Info:
            Public Key Algorithm: id-ecPublicKey
                Public-Key: (256 bit)
                pub:
                    04:a6:3d:01:58:f9:1c:1f:cd:ac:bb:d5:c6:3a:8f:
                    6b:37:96:6b:e7:e2:72:a7:af:0b:88:7c:be:aa:52:
                    20:1f:72:fe:38:08:ab:62:09:97:98:97:ee:37:02:
                    3c:4b:60:41:f1:cb:b4:1a:4d:f0:e5:c4:c5:ec:ed:
                    29:fe:a0:20:85
                ASN1 OID: prime256v1
                NIST CURVE: P-256
        X509v3 extensions:
            X509v3 Key Usage: critical
                Certificate Sign, CRL Sign
            X509v3 Basic Constraints: critical
                CA:TRUE
            X509v3 Subject Key Identifier:
                FE:B9:D6:0B:C6:CE:2C:25:4F:D8:59:CB:11:EA:A5:42:5F:8E:41:4B
            X509v3 Authority Key Identifier:
                keyid:B5:BE:84:8C:3C:72:23:E9:B2:22:30:DC:FB:B2:9B:88:DF:2E:F1:7A

            X509v3 Subject Alternative Name:
                DNS:pri-1dchdli.vault.ca.34a76791.consul, URI:spiffe://34a76791-b9b2-b93e-b0e4-1989ed11a28e.consul
<snip>
```

---

## Helm Reference Docs

The Helm reference docs (https://www.consul.io/docs/k8s/helm) are automatically
generated from our `values.yaml` file.

### Generating Helm Reference Docs

To generate the docs and update the `helm.mdx` file:

1. Fork `hashicorp/consul` (https://github.com/hashicorp/consul) on GitHub.
1. Clone your fork:
   ```shell-session
   git clone https://github.com/<your-username>/consul.git
   ```
1. Change directory into your `consul-k8s` repo:
   ```shell-session
   cd /path/to/consul-k8s
   ```
1. Run `make gen-helm-docs` using the path to your consul (not consul-k8s) repo:
   ```shell-session
   make gen-helm-docs consul=<path-to-consul-repo>
   # Examples:
   # make gen-helm-docs consul=/Users/my-name/code/hashicorp/consul
   # make gen-helm-docs consul=../consul
   ```
1. Open up a pull request to `hashicorp/consul` (in addition to your `hashicorp/consul-k8s` pull request)

### values.yaml Annotations

The code generation will attempt to parse the `values.yaml` file and extract all
the information needed to create the documentation but depending on the yaml
you may need to add some annotations.

#### @type
If the type is unknown because the field is `null` or you wish to override
the type, use `@type`:

```yaml
# My docs
# @type: string
myKey: null
```

#### @default
The default will be set to the current value but you may want to override
it for specific use cases:

```yaml
server:
  # My docs
  # @default: global.enabled
  enabled: "-"
```

#### @recurse
In rare cases, we don't want the documentation generation to recurse deeper
into the object. To stop the recursion, set `@recurse: false`.
For example, the ingress gateway ports config looks like:

```yaml
# Port docs
# @type: array<map>
# @default: [{port: 8080, port: 8443}]
# @recurse: false
ports:
- port: 8080
  nodePort: null
- port: 8443
  nodePort: null
```

So that the documentation can look like:
```markdown
- `ports` ((#v-ingressgateways-defaults-service-ports)) (`array<map>: [{port: 8080, port: 8443}]`) - Port docs
```

## Managing External CRD Dependencies

Some of the features of Consul on Kubernetes make use of CustomResourceDefinitions (CRDs) that we don't directly
manage. One such example is the Gateway API CRDs which we use to configure API Gateways, but are managed by SIG
Networking.

To pull external CRDs into our Helm chart and make sure they get installed, we generate their configuration using
[Kustomize](https://kustomize.io/) which can pull in Kubernetes config from external sources. We split these
generated CRDs into individual files and store them in the `charts/consul/templates` directory.

If you need to update the external CRDs we depend on, or add to them, you can do this by editing the
[control-plane/config/crd/external/kustomization.yaml](/control-plane/config/crd/external/kustomization.yaml) file.
Once modified, running

```bash
make generate-external-crds
```

will update the CRDs in the `/templates` directory.

## Adding a Changelog Entry

Any change that a Consul-K8s user might need to know about should have a changelog entry.

What doesn't need a changelog entry?
- Typos/fixes, unless they are in a public-facing API
- Code changes we are certain no Consul-K8s users will need to know about

To include a [changelog entry](../.changelog) in a PR, commit a text file
named `.changelog/<PR#>.txt`, where `<PR#>` is the number associated with the open
PR in GitHub. The text file should describe the changes in the following format:

````
```release-note:<change type>
<code area>: <brief description of the improvement you made here>
```
````

Valid values for `<change type>` include:
- `feature`: for the addition of a new feature
- `improvement`: for an improvement (not a bug fix) to an existing feature
- `bug`: for a bug fix
- `security`: for any Common Vulnerabilities and Exposures (CVE) resolutions
- `breaking-change`: for any change that is not fully backwards-compatible
- `deprecation`: for functionality which is now marked for removal in a future release

`<code area>` is meant to categorize the functionality affected by the change.
Some common values are:
- `cli`: related to the command-line interface and its commands
- `control-plane`: related to control-plane functionality
- `helm`: related to the charts module and any files, yaml, go, etc. therein

There may be cases where a `code area` doesn't make sense (i.e. addressing a Go CVE). In these
cases it is okay not to provide a `code area`.

For more examples, look in the [`.changelog/`](../.changelog) folder for existing changelog entries.

If a PR deserves multiple changelog entries, just add multiple entries separated by a newline
in the format described above to the `.changelog/<PR#>.txt` file.
