# Generate Helm reference docs from values.yaml and update Consul website.
# Usage: make gen-docs consul=<path-to-consul-repo>
gen-docs:
	@cd hack/helm-reference-gen; go run ./... $(consul)

# Deletes AWS resources left behind after failed acceptance tests.
ci.aws-acceptance-test-cleanup:
	@cd hack/aws-acceptance-test-cleanup; go run ./... -auto-approve
