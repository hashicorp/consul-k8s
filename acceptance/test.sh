gotest ./tests/peering -run TestPeering_Connect/secure_installation \
	-timeout 6h -p 1 -enable-enterprise -enable-multi-cluster \
	-enterprise-license $CONSUL_ENT_LICENSE \
	-kube-contexts "kind-dc1,kind-dc2" \
	-enable-transparent-proxy \
	-use-kind \
	-v 
