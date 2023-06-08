#!/bin/sh

rm -rf conformance
kind get clusters | grep test | while read -r line; do
  kind delete cluster --name $line
done