# Helm Charts

This folder contains the Helm charts for installing and configuring Consul
on Kubernetes. This chart supports multiple use cases of Consul on Kubernetes
depending on the values provided at install time.

## Prerequisites

To use the charts here, [Helm](https://helm.sh/) must be installed in your
Kubernetes cluster. This is well documented by Helm itself on the
[using Helm](https://docs.helm.sh/using_helm) page. Please follow that page
to install Helm into your cluster.

This folder contains a basic service account setup for initializing helm. For
demo purposes, you can quickly run the following to get up and running.
The steps below should be for demo, non-production usage only:

```
kubectl apply -f service-account.yaml
helm init --service-account helm
```

## Testing

Helm charts are tested in two forms: [Bats](https://github.com/bats-core/bats-core) tests and `helm test` tests.
The Bats tests test changing Helm chart values and the effect on the install.
The `helm test` tests verify that a deployed chart appears healthy.

To run the Bats test: `kubectl` must be configured locally to be authenticated
to a running Kubernetes cluster with Helm installed. With that in place,
just run bats:

    bats ./consul/test

If the tests fail, deployed resources in the Kubernetes cluster may not
be properly cleaned up. We recommend recycling the Kubernetes cluster to
start from a clean slate.

**Note:** There is a Terraform configuration in the
[terraform/ directory](https://github.com/hashicorp/consul-k8s/tree/master/terraform)
that can be used to quickly bring up a GKE cluster and configure
`kubectl` and `helm` locally. This can be used to quickly spin up a test
cluster.
