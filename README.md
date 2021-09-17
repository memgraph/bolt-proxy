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

## Authentication & Authorization

Currently bolt-proxy supports BasicAuth on and AADToken authentication for Azure.
To enable it set env variable `AUTH _METHOD` to the one of the possible authentication methods.

 - `AUTH_METHOD` -- currently only `BASIC_AUTH` and `AAD_TOKEN_AUTH` are supported

 Depending on the chosen authentication methods you will need to define specific environment variables:

 - `BASIC_AUTH_URL` -- URL against which to authenticate clients credentials
 - `AAD_TOKEN_CLIENT_ID` -- ClientID of the resource which you wish to authenticate against
 - `AAD_TOKEN_PROVIDER` -- The Azure authentication provider (e.g. https://login.microsoftonline.com/{tenant_name})

The user should use any client application (`mgconsole`, `neo4j-client`, `pymgclient`...) to connect to Memgraph and
send credentials via bolt protocol.
`mgconsole -username user -password password`
or
`mgconsole -username user -password JWT`

## Acknowledgments

Thanks to [Dave Voutila](https://github.com/voutilad) and his work on bolt-proxy for Neo4js
[bolt-proxy](https://github.com/voutilad/bolt-proxy) and for providing good base and inspiration for this bolt-proxy.

## License

[Apache License 2.0](https://github.com/memgraph/bolt-proxy/blob/main/LICENSE)
