# Consul Helm Chart

⭐ **We're looking for feedback on how folks are using Consul on Kubernetes. Please fill out our brief [survey](https://hashicorp.sjc1.qualtrics.com/jfe/form/SV_4MANbw1BUku7YhL)!** ⭐

This repository contains the official HashiCorp Helm chart for installing
and configuring Consul on Kubernetes. This chart supports multiple use
cases of Consul on Kubernetes, depending on the values provided.

For full documentation on this Helm chart along with all the ways you can
use Consul with Kubernetes, please see the
[Consul and Kubernetes documentation](https://www.consul.io/docs/platform/k8s/index.html).

## Prerequisites
  * **Helm 2.10+ or Helm 3.0+**
  * **Kubernetes 1.9+** - This is the earliest version of Kubernetes tested.
    It is possible that this chart works with earlier versions but it is
    untested.

## Usage

Detailed installation instructions for Consul on Kubernetes are found [here](https://www.consul.io/docs/k8s/installation/overview). 

1. Add the HashiCorp Helm Repository:
    
        $ helm repo add hashicorp https://helm.releases.hashicorp.com
        "hashicorp" has been added to your repositories
    
2. Ensure you have access to the consul chart: 

        $ helm search repo hashicorp/consul
        NAME                CHART VERSION   APP VERSION DESCRIPTION
        hashicorp/consul    0.20.1          1.7.2       Official HashiCorp Consul Chart

3. Now you're ready to install Consul! To install Consul with the default configuration using Helm 3 run:

        $ helm install consul hashicorp/consul --set global.name=consul
        NAME: consul

Please see the many options supported in the `values.yaml`
file. These are also fully documented directly on the
[Consul website](https://www.consul.io/docs/platform/k8s/helm.html).
