# Generate Helm reference docs from values.yaml and update Consul website.
# Usage: make gen-docs consul=<path-to-consul-repo>
gen-docs:
	@cd hack/helm-reference-gen; go run ./... $(consul)

# Copy generated CRD YAML into charts/consul.
# Usage: make copy-crds-to-chart
copy-crds-to-chart:
	@cd hack/copy-crds-to-chart; go run ./...

# Deletes AWS resources left behind after failed acceptance tests.
ci.aws-acceptance-test-cleanup:
	@cd hack/aws-acceptance-test-cleanup; go run ./... -auto-approve
