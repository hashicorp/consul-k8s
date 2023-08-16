#!/usr/bin/env bash

# Check terraform fmt
echo "==> Checking that code complies with terraform fmt requirements..."
tffmt_files=$(terraform fmt -check -recursive "$1")
if [[ -n ${tffmt_files} ]]; then
    echo 'terraform fmt needs to be run on the following files:'
    echo "${tffmt_files}"
    echo "You can use the command: \`make terraform-fmt\` to reformat all terraform code."
    exit 1
fi

echo "==> Check code compile completed successfully"
exit 0