### Instructions


1. Create cluster:
    `kind create cluster`

2. Load bolt-proxy image (suggested with kind):
    `kind load docker-image bolt-proxy:latest`

3. Apply configurations:
    `kubectl apply -f bolt-proxy-deploy.yml`
    `kubectl apply -f memgraph-deploy.yml`

4. Check service and pods and make sure everything is working:
    `kubectl get pod --watch`
    `kubectl get svc`

5. Get node IP, and NodePort exposed port:

    It should be only one node if you are testing this in local env):
        NODE_IP=`kubectl get node -o jsonpath='{ $.items[*].status.addresses[?(@.type=="InternalIP")].address }'`

    This gets the exposed port from the bolt-proxy:
        NODE_BOLT_PORT=`kubectl get svc bolt-proxy-controller -o go-template='{{range.spec.ports}}{{if .nodePort}}{{.nodePort}}{{"\n"}}{{end}}{{end}}'`

6. And now you should connect using any of the bolt clients, e.g. using `mgconsole`
    `mgconsole -host $NODE_IP -port $NODE_BOLT_PORT`


7. Or just run `run.sh` script.
