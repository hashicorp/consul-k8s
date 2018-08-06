# Consul + Kubernetes

This project is an official source of first-class integrations of Consul
with Kubernetes. The tools in this repository can be used for multiple
use cases from running Consul on Kubernetes to syncing services or
automatically enabling [Connect](https://www.consul.io/docs/connect/index.html).

The Kubernetes integrations with Consul are
[documented directly on the Consul website](https://www.consul.io/docs/kubernetes/index.html).
This README will present a basic overview of each use case, but for full
documentation please reference the Consul website.

## Examples Prerequisite

To try the Kubernetes integrations, you must first have a running
Kubernetes cluster. The Kubernetes website documents
[many ways to run Kubernetes](https://kubernetes.io/docs/setup/).
All the integrations are available via a [Helm chart](https://helm.sh/)
so Helm should also be installed on your Kubernetes system. Advanced users
may render the Helm chart locally and `kubectl apply` all the created
YAML files.

For this README, we'll use [GKE](https://cloud.google.com/kubernetes-engine/)
as an example to get started. This will assume that you have the
gcloud CLI installed and configured. Please refer to the
[GKE docs](https://cloud.google.com/kubernetes-engine/docs/quickstart) for
more information. **Warning:** using GKE as an example will cost some
money.

### Terraform

The `terraform/` folder contains a Terraform configuration that can be used to setup
an example cluster. This is the easiest and most automated way to setup a
demo cluster.

The pre-requisites for Terraform are:

  * Google Cloud authentication. See [Google Application Default Credentials](https://cloud.google.com/docs/authentication/production). You may also reuse your `gcloud` credentials by exposing them as application defaults by running `gcloud auth application-default login`.
  * `gcloud` installed and configured locally with GKE components.
  * The following programs available on the PATH: `kubectl`, `helm`, `grep`, `xargs`.

With that available, run the following in that directory:

```
$ terraform init
$ terraform apply
```

The apply will ask you for the name of the project to setup the cluster.
After this, everything will be setup, your local `kubectl` credentials will
be configured, and you may use `helm` in the examples below.

### Manual, Shell

Create a Kubernetes cluster:

```
gcloud container clusters create consul-k8s-example \
    --enable-legacy-authorization \
    --cluster-version=1.10.5-gke.0 \
    --num-nodes=3 \
    --machine-type=n1-standard-2
```

Setup credentials:

```
gcloud container clusters get-credentials consul-k8s-example
```

Validate that the cluster is ready:

```
kubectl get componentstatus
```

Setup Helm:

```
kubectl apply -f helm/service-account.yaml
helm init --service-account helm
```

With a Kubernetes cluster up and running with Helm installed, you may now run
the examples for the use cases below. Don't forget to destroy your example cluster
when you're done.

## Use Cases

TODO
