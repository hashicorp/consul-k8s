FROM hashicorp/consul-k8s:latest

# change the user to root so we can install stuff
USER root
RUN apk update && apk add iptables
USER consul-k8s

COPY pkg/bin/linux_amd64/consul-k8s /bin
