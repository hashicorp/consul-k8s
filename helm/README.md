# Helm Charts

This folder contains the Helm charts for installing and configuring Consul
on Kubernetes. This chart supports multiple use cases of Consul on Kubernetes
depending on the values provided at install time.

```
kubectl apply -f service-account.yaml
helm init --service-account helm
```
