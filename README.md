# Consul Helm Chart

## ⚠️	 ANNOUNCEMENT: Consul Helm Chart moving to `hashicorp/consul-k8s` ⚠️	

We are planning on consolidating our Consul Helm and Consul K8s repos soon! *TLDR:* The HashiCorp Helm chart will be moving to [`hashicorp/consul-k8s`](https://github.com/hashicorp/consul-k8s). We target completing the migration before the end of August 2021.

### Background

For users, the separate repositories lead to difficulty on new releases and confusion surrounding versioning. Most of the time new releases that include changes to `consul-k8s` also change `consul-helm`. But separate repositories mean separate GitHub PR's and added confusion in opening new Github Issues. In addition, we maintain separate versions of the `consul-k8s` binary and the Consul Helm chart, which in most cases are more tightly coupled together with dependencies. This versioning strategy has also led to confusion as to which Helm charts are compatible with which versions of `consul-k8s`.

### Proposal

The single repository name will be consul-k8s. It will contain the Helm charts, control-plane code, and other components of Consul on Kubernetes. The original `consul-k8s` binary will be renamed to `consul-k8s-control-plane`, and an upcoming user-facing CLI binary will be called `consul-k8s`.

Some additional details regarding the Consul Helm chart migration:

1. All open and closed issues will be migrated from `consul-helm` to `consul-k8s`.
2. PRs from consul-helm will not be migrated `consul-k8s`. Users will need to do that themselves, since it would be too complex to perform the migration with CI automation. The new repo will have a charts folder where the Helm chart will reside and PRs can be made against the contents in that folder in the same structure as before.
3. We will be archiving the `consul-helm` repo, as no more changes will be made to the repository.
4. A new Docker image that hosts the renamed consul-k8s binary will now exist in the following Docker Hub repo: `hashicorp/consul-k8s-control-plane`.
5. Most importantly, there will be no change to any users that are installing Consul Kubernetes via the Helm chart, since the Helm chart will still be released to same location (i.e. https://helm.releases.hashicorp.com).

After the integration, all of our Consul on Kubernetes components will be versioned together. For each new release of Consul Kubernetes, a new tag of the repository will be created. We will use that tag as the version for the control-plane docker image, the upcoming CLI binary, and the Consul Helm charts.

---

 **We're looking for feedback on how folks are using Consul on Kubernetes. Please fill out our brief [survey](https://hashicorp.sjc1.qualtrics.com/jfe/form/SV_4MANbw1BUku7YhL)!** 
 
----

## Overview

This repository contains the official HashiCorp Helm chart for installing
and configuring Consul on Kubernetes. This chart supports multiple use
cases of Consul on Kubernetes, depending on the values provided.

For full documentation on this Helm chart along with all the ways you can
use Consul with Kubernetes, please see the
[Consul and Kubernetes documentation](https://www.consul.io/docs/platform/k8s/index.html).

## Prerequisites
  * **Helm 3.0+** (Helm 2 is not supported)
  * **Kubernetes 1.17+** - This is the earliest version of Kubernetes tested.
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

## Tutorials

You can find examples and complete tutorials on how to deploy Consul on 
Kubernetes using Helm on the [HashiCorp Learn website](https://learn.hashicorp.com/consul).
