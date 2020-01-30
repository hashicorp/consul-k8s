FROM circleci/golang:1.13
COPY --from=hashicorp/consul-enterprise:1.7.0-ent-beta3 /bin/consul /bin/consul
