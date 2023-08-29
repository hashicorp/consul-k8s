#!/usr/bin/env bash

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)

set -eux

TEMPLATES="${WD}/../templates"
DASHBOARDS="${WD}/dashboards"
TMP=$(mktemp -d)

# create Prometheus template
helm template prometheus prometheus \
  --repo https://prometheus-community.github.io/helm-charts \
  --namespace "replace-me-namespace" \
  --version 13.2.1 \
  -f "${WD}/values/prometheus.yaml" \
  > "${TEMPLATES}/prometheus.yaml"

# Find and replace `replace-me-namespace` with `{{ .Release.Namespace }}` in Prometheus template.
sed -i'.orig' 's/replace-me-namespace/{{ .Release.Namespace }}/g' "${TEMPLATES}/prometheus.yaml"
# Add a comment to the top of the template file mentioning that the file is auto-generated.
sed -i'.orig' '1i\
# This file is auto-generated, see addons/gen.sh
' "${TEMPLATES}/prometheus.yaml"
# Add `{{- if .Values.prometheus.enabled }} to the top of the Prometheus template to ensure it is only templated when enabled.
sed -i'.orig' '1i\
{{- if .Values.prometheus.enabled }}
' "${TEMPLATES}/prometheus.yaml"
# Add `{{- end }} to the bottom of the Prometheus template to ensure it is only templated when enabled (closes the `if` statement).
sed -i'.orig' -e '$a\
{{- end }}' "${TEMPLATES}/prometheus.yaml"
# Remove the `prometheus.yaml.orig` file that is created as a side-effect of the `sed` command on OS X.
rm "${TEMPLATES}/prometheus.yaml.orig"