FROM hashicorp/consul-k8s-control-plane:latest
COPY pkg/bin/linux_amd64/consul-k8s-control-plane /bin
