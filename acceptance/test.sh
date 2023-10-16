gotest ./tests/peering -run TestPeering_Connect2 \
	-timeout 1h -p 1 -enable-enterprise -enable-multi-cluster \
	-enterprise-license $CONSUL_ENT_LICENSE \
	-kube-contexts "eks-dc1,eks-dc2" \
	-v 
