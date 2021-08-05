# Contributing

## Rebasing contributions against master

PRs in this repo are merged using the [`rebase`](https://git-scm.com/docs/git-rebase) method. This keeps
the git history clean by adding the PR commits to the most recent end of the commit history. It also has
the benefit of keeping all the relevant commits for a given PR together, rather than spread throughout the
git history based on when the commits were first created.

If the changes in your PR do not conflict with any of the existing code in the project, then Github supports
automatic rebasing when the PR is accepted into the code. However, if there are conflicts (there will be
a warning on the PR that reads "This branch cannot be rebased due to conflicts"), you will need to manually
rebase the branch on master, fixing any conflicts along the way before the code can be merged.

## Testing

The Helm chart ships with both unit and acceptance tests.

The unit tests don't require any active Kubernetes cluster and complete
very quickly. These should be used for fast feedback during development.
The acceptance tests require a Kubernetes cluster with a configured `kubectl`.

### Prequisites
* [Bats](https://github.com/bats-core/bats-core)
  ```bash
  brew install bats-core
  ```
* [yq](https://pypi.org/project/yq/)
  ```bash
  brew install python-yq
  ```
* [Helm 3](https://helm.sh) (Helm 2 is not supported)
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

    bats ./test/unit

To run tests in a specific file:

    bats ./test/unit/<filename>.bats

To run tests in parallel use the `--jobs` flag (requires parallel `brew install parallel`):

    bats ./test/unit/<filename>.bats --jobs 8

To run a specific test by name use the `--filter` flag:

    bats ./test/unit/<filename>.bats --filter "my test name"

#### Acceptance Tests

To run the acceptance tests:

    cd test/acceptance/tests
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
        -kubecontext=<name of the primary Kubernetes context> \
        -secondary-kubecontext=<name of the secondary Kubernetes context>

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
-kubeconfig string
    The path to a kubeconfig file. If this is blank, the default kubeconfig path (~/.kube/config) will be used.
-kubecontext string
    The name of the Kubernetes context to use. If this is blank, the context set as the current context will be used by default.
-namespace string
    The Kubernetes namespace to use for tests. (default "default")
-no-cleanup-on-failure
    If true, the tests will not cleanup Kubernetes resources they create when they finish running.Note this flag must be run with -failfast flag, otherwise subsequent tests will fail.
-secondary-kubeconfig string
    The path to a kubeconfig file of the secondary k8s cluster. If this is blank, the default kubeconfig path (~/.kube/config) will be used.
-secondary-kubecontext string
    The name of the Kubernetes context for the secondary cluster to use. If this is blank, the context set as the current context will be used by default.
-secondary-namespace string
    The Kubernetes namespace to use in the secondary k8s cluster. (default "default")
```

**Note:** There is a Terraform configuration in the
[`test/terraform/gke`](./test/terraform/gke) directory
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

We recommend to start by either copying the [example test](test/acceptance/tests/example/example_test.go)
or the whole [example test suite](test/acceptance/tests/example),
depending on the test you need to add.

#### Adding Test Suites

To add a test suite, copy the [example test suite](test/acceptance/tests/example)
and uncomment the code you need in the [`main_test.go`](test/acceptance/tests/example/main_test.go) file.

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

We recommend using the [example test](test/acceptance/tests/example/example_test.go)
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

Please see [mesh gateway tests](test/acceptance/tests/mesh-gateway/mesh_gateway_test.go)
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
 
## Helm Reference Docs
 
The helm reference docs (https://www.consul.io/docs/k8s/helm) are automatically
generated from our `values.yaml` file.

### Generating Helm Reference Docs
 
To generate the docs and update the `helm.mdx` file:

1. Fork `hashicorp/consul` (https://github.com/hashicorp/consul) on GitHub
1. Clone your fork:
   ```shell-session
   git clone https://github.com/your-username/consul.git
   ```
1. Change directory into your `consul-helm` repo: 
   ```shell-session
   cd /path/to/consul-helm
   ```
1. Run `make gen-docs` using the path to your consul (not consul-helm) repo:
   ```shell-session
   make gen-docs consul=<path-to-consul-repo>
   # Examples:
   # make gen-docs consul=/Users/my-name/code/hashicorp/consul
   # make gen-docs consul=../consul
   ```
1. Open up a pull request to `hashicorp/consul` (in addition to your `hashicorp/consul-helm` pull request)

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
