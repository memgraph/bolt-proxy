#!/bin/bash

set euo pipefail

does_command_exist() {
    local command=${1}
    if ! command -v ${command} &> /dev/null
    then
        echo "${command} could not be found"
        exit
    fi
}

does_command_exist kind
does_command_exist kubectl

kind create cluster
kind load docker-image bolt-proxy:latest
kubectl apply -f bolt-proxy-deploy.yml
    kubectl apply -f memgraph-deploy.yml

kubectl wait --for=condition=available --timeout=90s deployment/bolt-proxy
kubectl wait --for=condition=available --timeout=120s deployment/memgraph

NODE_IP=$(kubectl get node -o jsonpath='{ $.items[*].status.addresses[?(@.type=="InternalIP")].address }')
NODE_BOLT_PORT=$(kubectl get svc bolt-proxy-controller -o go-template='{{range.spec.ports}}{{if .nodePort}}{{.nodePort}}{{"\n"}}{{end}}{{end}}')

echo "###############"
echo "IP: ${NODE_IP}, PORT: ${NODE_BOLT_PORT}"
echo "###############"
echo "To connect to memgraph run the following command:"
echo "mgconsole -host ${NODE_IP} -port ${NODE_BOLT_PORT}"
