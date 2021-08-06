FROM hashicorp/consul-k8s:latest

# change the user to root so we can install stuff
USER root
RUN rm /bin/consul-k8s
RUN apk update && apk add iptables

COPY pkg/bin/linux_amd64/consul-k8s-control-plane /bin
RUN ln -s /bin/consul-k8s-control-plane /bin/consul-k8s

USER consul-k8s
