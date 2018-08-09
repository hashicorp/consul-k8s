#!/bin/bash

kubectl get configmap \
    -n kube-system extension-apiserver-authentication \
    -o=jsonpath='{.data.client-ca-file}' | \
    base64 | \
    tr -d '\n'
