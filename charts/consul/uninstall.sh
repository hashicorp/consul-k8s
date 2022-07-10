#!/bin/sh

kubectl delete -f ./examples/example.yaml
kubectl delete -f ./examples/ingress.yaml

helm uninstall consul-blue-client -n consul-blue-client
helm uninstall consul-green-client -n consul-green-client

helm uninstall consul-blue -n consul-blue
kubectl delete pvc --selector="chart=consul-helm" -n consul-blue

helm uninstall consul-green -n consul-green
kubectl delete pvc --selector="chart=consul-helm" -n consul-green

