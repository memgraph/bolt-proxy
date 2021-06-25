# Bolt-proxy

If you wish to support bolt protocol in your kubernetes cluster and authenticate them via ingress service, this bolt-proxy help you intercept those requests and delegate authentication.
This projects aims to help everyone using k8 clusters to use this bolt-proxy in order to implement cluster authentication inside of it.

## How to use?

You can set up these flags manually:
```
Usage of ./bolt-proxy:
  -bind string
        host:port to bind to (default "localhost:8888")
  -cert string
        x509 certificate
  -debug
        enable debug logging
  -key string
        x509 private key
  -pass string
        Memgraph password
  -uri string
        bolt uri for remote Memgraph (default "bolt://localhost:7687")
  -user string
        Memgraph username (default "")
```

or set up the env variables:

- `BOLT_PROXY_BIND` -- host:port to bind to (e.g. "0.0.0.0:8888")
- `BOLT_PROXY_URI` -- bolt uri for backend system(s) (e.g. "bolt://host-1:7687")
- `BOLT_PROXY_USER` -- memgraph user for the backend monitor
- `BOLT_PROXY_PASSWORD` -- password for the backend memgraph user for use by the monitor
- `BOLT_PROXY_CERT` -- path to the x509 certificate (.pem) file
- `BOLT_PROXY_KEY` -- path to the x509 private key file
- `BOLT_PROXY_DEBUG` -- set to any value to enable debug mode/logging

# Acknowledgments
Thanks to [Dave Voutila](https://github.com/voutilad) adn his work on bolt-proxy for neo4js [bolt-proxy](https://github.com/voutilad/bolt-proxy) for providing good base and inspiration for this bolt-proxy.