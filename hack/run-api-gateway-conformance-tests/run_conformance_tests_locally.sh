#!/bin/sh

#test suite assumes that kind cluster is already spun up with metal lb installed
#this step will initiate the cluster set up piece locally, but won't automatically
#tear down kind cluster

#TODO generate this dynamically
consulImage="hashicorppreview/consul-enterprise:1.16-dev"

echo creating kind cluster
testID=$RANDOM
clusterName="consul-api-gateway-conformance-test-$testID"
imageName="consul-k8s:test"
workdir="./conformance"
mkdir $workdir


kind create cluster --name $clusterName
#--config cluster.yaml




##build image and load into cluster
make DEV_IMAGE=$imageName control-plane-dev-docker
kind load docker-image $imageName $imageName --name $clusterName

#add consul license
echo creating secret
kubectl create namespace consul
secret=$(echo $CONSUL_LICENSE)
kubectl create secret generic consul-ent-license --from-literal="key=${secret}" --namespace consul


#clone
git clone https://github.com/kubernetes-sigs/gateway-api.git
mv ./gateway-api $workdir/gateway-api

#set up config like the CI has to
cat <<EOF > "$workdir/consul-config.yaml"
global:
  tls:
    enabled: true
  enterpriseLicense:
    secretName: 'consul-ent-license'
    secretKey: 'key'
server:
  replicas: 1
connectInject:
  enabled: true
  default: true
controller:
  enabled: true
logLevel: info
EOF

cat <<EOF > "$workdir/metallb-config.yaml"
apiVersion: v1
kind: ConfigMap
metadata:
  namespace: metallb-system
  name: config
data:
  config: |
    address-pools:
      - name: default
      protocol: layer2
      addresses:
      - 172.18.255.200-172.18.255.250
EOF

cat <<EOF > "$workdir/kustomization.yaml"
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - ./base/manifests.yaml
  - ./proxydefaults.yaml

patches:
  # Add connect-inject annotation to each Deployment. This is required due to
  # containerPort not being defined on Deployments upstream. Though containerPort
  # is optional, Consul relies on it as a default value in the absence of a
  # connect-service-port annotation.
  - patch: |-
      - op: add
        path: "/spec/template/metadata/annotations"
        value: {'consul.hashicorp.com/connect-service-port': '3000'}
    target:
      kind: Deployment
  # We don't have enough resources in the GitHub-hosted Actions runner to support 2 replicas
  - patch: |-
      - op: replace
        path: "/spec/replicas"
        value: 1
    target:
      kind: Deployment
EOF


cat <<EOF > "$workdir/proxydefaults.yaml"
apiVersion: consul.hashicorp.com/v1alpha1
kind: ProxyDefaults
metadata:
  name: global
  namespace: consul
spec:
  config:
    protocol: http
EOF


kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.13.10/config/manifests/metallb-native.yaml
kubectl apply -f $workdir/metallb-config.yaml
kubectl wait --for=condition=Ready --timeout=60s --namespace=metallb-system pods --all


#echo helm install
helm install --values "$workdir"/consul-config.yaml consul ./charts/consul --namespace consul --set global.imageK8S="$imageName" --set global.image="$consulImage"

cp $workdir/kustomization.yaml $workdir/gateway-api/conformance/
cp $workdir/proxydefaults.yaml $workdir/gateway-api/conformance/
ls $workdir/gateway-api/conformance/
cd $workdir/gateway-api/conformance/
kubectl kustomize ./ --output ./base/manifests.yaml

kubectl wait --for=condition=Ready --timeout=60s  pods --all --namespace consul

go test -v -timeout 10m ./ --gateway-class=consul

rm -rf $workdir

