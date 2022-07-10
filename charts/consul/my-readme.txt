#  Multi-Version K8s Consul will not work with Hashicorp provided helm chart.  This is because the hostPorts for daemonset (HTTP, HTTPS, GRPC) are hard coded.  This fork addresses this issue by parameterizing the hostPorts while keeping the defauls.

vendor install instructions
https://www.consul.io/docs/k8s/installation/install

setup local k8s;  I use Docker Desktop and Kind.
install kind / calico;  see kind folder

To search different versions of helm charts
helm search repo hashicorp/consul -l

Run ./install.sh
This install script will install consul blue, green, ingress and sample application.  see example folder for manifests.

-----------------------

Consul UI: 
Consul-Blue:  http://consul-blue.dvp.com
Consul-Green:  http://consul-green.dvp.com

Note:  Be sure to setup hostnames to point to localhost

-----------------------

from curl container: 

curl -X PUT -d 'bar' 'http://consul-blue-server.consul-blue.svc.cluster.local:8500/v1/kv/foo'
curl http://consul-blue-server.consul-blue.svc.cluster.local:8500/v1/kv/foo
curl -X DELETE 'http://consul-blue-server.consul-blue.svc.cluster.local:8500/v1/kv/foo'

curl -X PUT -d 'xyz' 'http://consul-green-server.consul-green.svc.cluster.local:8500/v1/kv/abc'
curl http://consul-green-server.consul-green.svc.cluster.local:8500/v1/kv/abc
curl -X DELETE 'http://consul-green-server.consul-green.svc.cluster.local:8500/v1/kv/abc'

-----------------------

from client agent:
kubectl exec -it < consul-green-client-pod > --namespace consul-green-client -- /bin/sh

At command prompt, type consul commands.
consul members
consul kv put foo bar
consul kv get foo


-----------------------

Cleanup

./uninstall
