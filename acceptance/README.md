# Consul on Kubernetes Acceptance Tests

Acceptance tests for Consul on Kubernetes validate that the "happy path" of features for the product integrate together. They help to ensure that changes in one part of the codebase do not create bugs in another, disparate part of the codebase.

To do this, they run on a live Kubernetes cluster, either locally or in a cloud servive like EKS.
A subset of these tests is run on every pull request. 

## Running Tests Locally

Contributors will often find it useful to run a subset of acceptance tests on their local machine in order to verify that a code change they have made does not break Consul on Kubernetes. When running tests on the local machine, the Kubernetes cluster where the test is executed may also be running locally or may be running in a cloud service. Because the acceptance tests leverage Kubeconfig to determine the target cluster, unless otherwise specified, the context used for running tests will be the same as what is returned from `kubectl config current-context`

### Setting up an environment for running acceptance tests

These steps will take you through setting up an environment locally on your machine to run acceptance tests. This includes running the Kubernetes cluster on your machine using Docker. If you wish to run your tests against a particular cloud distribution (AKS, EKS, GKE, etc.), you can skip the section on standing up a local Kubernetes cluster and by default the tests will use the context configured in your Kubeconfig.

#### Requirements

Running acceptance tests requires tooling to build the image you want to test as well as run a local instance of Kubernetes to execute the tests in.

- [Docker Engine](https://docs.docker.com/engine/install/) for building container images and running Kubernetes locally.
- [gox](https://github.com/mitchellh/gox) for building cross-platform Go binaries.  
  Installing with Go.
  ```bash
  go install https://github.com/mitchellh/gox
  ```

  Installing with Homebrew.
  ```bash
  brew install gox
  ```
- [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) (Kubernetes in Docker) for running Kubernetes clusters
  using container images.
- [Kubectl](https://kubernetes.io/docs/tasks/tools/) which is used by tests to interact with the Kubernetes API.
- [k9s](https://k9scli.io/) (Optional) a useful tool for observing Kubernetes clusters while the tests run.

#### Standing up a local Kubernetes cluster

Developers on this project commonly use Kind to run local instances of Kubernetes. Some acceptance tests use only a single cluster to execute. Others, which test connections between clusters, use two clusters.

Create a Kind cluster.

```bash
kind create cluster -n <NAME>
```

Multi-cluster tests generally define a `"dc1"` and `"dc2"` cluster.

```bash
kind create cluster -n dc1
kind create cluster -n dc2
```

When you create a cluster with Kind, the config is added to your Kubeconfig and can be accessed using Kubectl.

```bash
kubectl config use-context kind-<NAME>
```

#### Building Docker Images for Testing

Before running a test, you will need to compile Docker images for the components you want to test. The configuration for the acceptance tests will take an image name to configure the components that make up Consul on Kubernetes. 

Consul on Kubernetes orchestrates the interactions between 3 main components:

- Consul on Kubernetes Control Plane (`consul-k8s-control-plane`): This component is the heart of the Consul on Kubernetes repository and is responsible for controlling the interactions between Consul servers and dataplane instances as well as configuring ACLs, syncing custom resources as Consul config entries, and updating the Consul service mesh to reflect deployments of service endpoints in Kubernetes. This will be the component you most often need to compile as its development has the widest impact on the behavior of the whole system.
- Consul: Either community edition or enterprise, this component is used to run servers that store mesh state and communicate with Consul Dataplane instances to configure their behavior using Envoy's xDS protocol. In older versions of Consul on Kubernetes, this component was also used to mediate between Consul servers and Envoy sidecars. This model was replaced with the direct interaction of Dataplanes and Consul Servers in the "agentless" architecture starting with Consul on Kubernetes `1.0.0`.
- Consul Dataplane: This component is injected by the Control Plane. It is responsible for connecting to Consul servers and responding to xDS configuration to route traffic through the service mesh. It wraps Envoy which handles requests.

It is only necessary to compile an image for the components that your change affected. That is, if you only made a change to the Consul on Kubernetes Control Plane, you don't need to compile your own image for Consul Dataplane or Consul itself.

Compile an image for Consul on Kubernetes Control Plane. From the [Consul on Kubernetes Repository]() (this repository):

```bash
make control-plane-dev-docker
```

Compile an image for Consul community edition. From the [Consul Repository]() root:

```bash
make dev-docker
```

Compile an image for Consul enterprise. From the [Consul Enterprise Repository] root:

```bash
make dev-docker
```

Compile an image for Consul Dataplane. From the [Consul Dataplane Repository] root:

```bash
make dev-docker
```

#### Loading Your Image into Kind

Regardless of which image your have built, to run that image within Kind, you can either push it to a Docker registry or load it into the Kind cluster itself. It is easier to do the latter method and reduces the overhead of pushing to an external image repository like DockerHub.

```bash
kind load docker-image <FULL-NAME-OF-IMAGE> -n <KIND-CLUSTER-NAME>
```

#### Running a Basic Test

The acceptance test framework is highly configurable at runtime. Therefore, it is easier to get a sense of how the framework works by starting with an example rather than trying to understand the whole framework at once.

Let's look at the case where a developer has made a change to `consul-k8s-control-plane` and wants to ensure that their change does not break service mesh behavior when TLS is not enabled. 

```bash
go test -run TestConnectInject/not-secure ./connect \
    -use-kind -timeout 1h -p 1 \
    -consul-dataplane-image consul-dataplane/release-default:local
```

#### Running Multi-cluster Tests

#### All Test Flags

The table below lists all flags that may be passed into the testing framework in order to configure runtime behavior. If a flag does not have a "type" listed, the presence of the flag itself toggles the behavior. For example, to run enterprise tests, passing `-enable-enterprise` alone is sufficient.

```text
Flag                           Type     Description
-consul-image                  string   The Consul image to use for all tests.
-enable-enterprise                      The test suite will run tests for enterprise features. Note that some 
                                          features may require setting the enterprise license flag below or the env
                                          var CONSUL_ENT_LICENSE.
-enterprise-license            string   The enterprise license for Consul.
-enable-multi-cluster                   The tests that require multiple Kubernetes clusters will be run. At least 
                                          one of -secondary-kubeconfig or -secondary-kubecontext is required when 
                                          this flag is used.
-enable-openshift                       The tests will automatically add Openshift Helm value for each Helm install.
-enable-pod-security-policies           The test suite will run tests with pod security policies enabled.
-enable-transparent-proxy               The test suite will run tests with transparent proxy enabled. This only 
                                          affects tests that enable connectInject.
-kubeconfigs                   string   The comma separated list of Kubernetes configs to use 
                                          (eg. "~/.kube/config,~/.kube/config2"). The first in the list will be 
                                          treated as the primary config, followed by the secondary, etc. If the list 
                                          is empty, or items are blank, then the default kubeconfig path 
                                          (~/.kube/config) will be used.
-kube-contexts                 string   The comma separated list of Kubernetes contexts to use 
                                          (eg. "kind-dc1,kind-dc2"). The first in the list will be treated as the 
                                          primary context, followed by the secondary, etc. If the list is empty, or 
                                          items are blank, then the current context will be used.
-kube-namespaces               string   The comma separated list of Kubernetes namespaces to use 
                                          (eg. "consul,consul-secondary"). The first in the list will be treated as
                                          the primary namespace, followed by the secondary, etc. If the list is 
                                          empty, or fields are blank, then the current namespace will be used.
-debug-directory               string   The directory where to write debug information about failed test runs, such 
                                          as logs and pod definitions. If not provided, a temporary directory will
                                          be created by the tests.
-no-cleanup-on-failure                  The tests will not cleanup Kubernetes resources they create when they finish 
                                          running. Note this flag must be run with -failfast flag, otherwise 
                                          subsequent tests will fail.
```

## Writing New Acceptance Tests


