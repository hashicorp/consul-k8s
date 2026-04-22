ocp upgrade from 4.18 to 4.19: flag upgradefrom418to419: true
case 1: No TCP GVK; in helm we should set crds.enableTcpRoute to false/true

by default:
1. as pre-upgrade hook: we generate manifests, updates them to API version from gateway.networking.k8s.io/v1beta1 for gateways,gatewayclasses, httproutes, grpcroutes to gateway.networking.k8s.io/v1 ; v1beta1 for referencegrants to v1beta1 only and delete the TCP CRD if present and owned by it and store in the pvc /data/gw.
2. our controller will not watch TCP gvk any more.
3. We apply the manifests.

iff helm values for crds.consulapi.enabled is true
1. as pre-upgrade hook: we generate manifests, updates them to API version from gateway.networking.k8s.io/v1beta1 for gateways,gatewayclasses, httproutes, grpcroutes to consul.hashicorp.com/v1beta1 ; gateway.networking.k8s.io/v1beta1 for referencegrants to consul.hashicorp.com/v1beta1 only and update gateway.networking.k8s.io/v1alpha2 of tcproutes to consul.hashicorp.com/v1beta1and store in the pvc under /data/consul. 
We also generate manifests, updates them to API version from gateway.networking.k8s.io/v1beta1 for gateways,gatewayclasses, httproutes, grpcroutes to gateway.networking.k8s.io/v1 ; gateway.networking.k8s.io/v1beta1 for referencegrants to cgateway.networking.k8s.io/v1beta1 only and delete the TCP CRD if present and owned by it and store in the pvc /data/gw.
2. new controller will watch for consul.hashicorp.com API group and old controller watch for gateway*
3. we apply the manifests under /data/gw and /data/consul
4. Customer should delete the objects under api group gateway.networking.k8s.io after dns update

case 2: if TCP GVK is present, we mandate custom consul crds to be used <mandate logic should be done>

1. as pre-upgrade hook: we generate manifests updates them to API version from gateway.networking.k8s.io/v1beta1 for gateways,gatewayclasses, httproutes, grpcroutes to consul.hashicorp.com/v1beta1 or gateway.networking.k8s.io/v1beta1 for referencegrants to consul.hashicorp.com/v1beta1 only and update gateway.networking.k8s.io/v1alpha2 of tcproutes to consul.hashicorp.com/v1beta1 and store in the pvc /data/consul.
We also generate manifests, updates them to API version from gateway.networking.k8s.io/v1beta1 for gateways,gatewayclasses, httproutes, grpcroutes to gateway.networking.k8s.io/v1 ; gateway.networking.k8s.io/v1beta1 for referencegrants to cgateway.networking.k8s.io/v1beta1 only and delete the tcproutes object if present, uses the TCP GVK owned by it and store in the pvc /data/gw.

2. new controller will watch for consul.hashicorp.com API group and old controller watch for gateway*
3. We apply the manifests under /data/consul and /data/gw.
4. Customer should delete the objects under api group gateway.networking.k8s.io after dns update (not tcp does work only under new API group)


for non-ocp upgrade:

case 1: No TCP GVK; in helm we should set crds.enableTcpRoute to false
as pre-upgrade hook: we generate manifests, updates them to API version from gateway.networking.k8s.io/v1beta1 for gateways,gatewayclasses, httproutes, grpcroutes to gateway.networking.k8s.io/v1 or v1beta1 for referencegrants to v1beta1 only and delete the TCP CRD if present and owned by it and store in the pvc.
2. our controller will not watch TCP gvk any more.
3. We apply the manifests

case 2: No TCP GVK; in helm we should set crds.enableTcpRoute to true
as pre-upgrade hook: we generate manifests, updates them to API version from gateway.networking.k8s.io/v1beta1 for gateways,gatewayclasses, httproutes, grpcroutes to gateway.networking.k8s.io/v1 or v1beta1 for referencegrants to v1beta1 only and update gateway.networking.k8s.io/v1alpha2 of tcproutes to gateway.networking.k8s.io/v1alpha2 only and store in the pvc.
2. our controller will watch TCP gvk.
3. We apply the manifests

case 3: TCP gvk; in helm we should set crds.enableTcpRoute to true
as pre-upgrade hook: we generate manifests, updates them to API version from gateway.networking.k8s.io/v1beta1 for gateways,gatewayclasses, httproutes, grpcroutes to gateway.networking.k8s.io/v1 or v1beta1 for referencegrants to v1beta1 only and update gateway.networking.k8s.io/v1alpha2 of tcproutes to gateway.networking.k8s.io/v1alpha2 only and store in the pvc.
2. our controller will watch TCP gvk.
3. We apply the manifests

case 4: TCP gvk; in helm we should set crds.enableTcpRoute to false
    not recommended

case 5: when crds.consulapi.enabled is set to true.
1. as pre-upgrade we generate the manifests from existing objects as above, store manifests of the gateway.neetworking.k8s.io under /data/gw and consul.hashicorp.com under /data/consul.
2. we apply both the manifest directory
3. new controller watch consul* APi and old controller watch for gateway*
3. customer should delete the objects under gateway.* API after updating dns
