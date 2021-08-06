TEST_IMAGE?=consul-helm-test

test-docker:
	@docker build --rm -t '$(TEST_IMAGE)' -f $(CURDIR)/test/docker/Test.dockerfile $(CURDIR)

# Generate Helm reference docs from values.yaml and update Consul website.
# Usage: make gen-docs consul=<path-to-consul-repo>
gen-docs:
	@cd hack/helm-reference-gen; go run ./... $(consul)

.PHONY: test-docker
