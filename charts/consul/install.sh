#!/bin/sh

#  Install blue server
helm install consul-blue ./  --create-namespace --namespace consul-blue --version "0.45.0" --values ./examples/values-blue.yaml

#  Install green server
helm install consul-green ./  --create-namespace --namespace consul-green --version "0.44.0" --values ./examples/values-green.yaml

#  Install blue client agent; default ports
helm install consul-blue-client ./  --create-namespace --namespace consul-blue-client --version "0.45.0" --values ./examples/values-blue-client.yaml

#  Install green client agent; custom daemonset ports:  http - 9500, grpc 9502
helm install consul-green-client ./  --create-namespace --namespace consul-green-client --version "0.44.0" --values ./examples/values-green-client.yaml

#  wait for clients to come up.
sleep 45

#  install Blue / Green UI and sample app
kubectl apply -f ./examples/ingress.yaml
kubectl apply -f ./examples/example.yaml
