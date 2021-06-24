package main

import (
	"crypto/tls"
	"flag"
	"io/ioutil"
	"net"
	"os"

	// debuggin' -- used for runtime profiling/debugging

	_ "net/http/pprof"

	"github.com/memgraph/bolt-proxy/backend"
	"github.com/memgraph/bolt-proxy/frontend"
	"github.com/memgraph/bolt-proxy/proxy_logger"
)

type Parameters struct {
	debugMode          bool
	bindOn             string
	proxyTo            string
	username, password string
	certFile, keyFile  string
}

const (
	DEFAULT_BIND string = "localhost:8888"
	DEFAULT_URI  string = "bolt://localhost:7687"
	DEFAULT_USER string = "neo4j"
)

var proxy_params Parameters

func init() {
	var (
		debugMode          bool
		bindOn             string
		proxyTo            string
		username, password string
		certFile, keyFile  string
	)

	bindOn, found := os.LookupEnv("BOLT_PROXY_BIND")
	if !found {
		bindOn = DEFAULT_BIND
	}
	proxyTo, found = os.LookupEnv("BOLT_PROXY_URI")
	if !found {
		proxyTo = DEFAULT_URI
	}
	username, found = os.LookupEnv("BOLT_PROXY_USER")
	if !found {
		username = DEFAULT_USER
	}
	_, debugMode = os.LookupEnv("BOLT_PROXY_DEBUG")
	password = os.Getenv("BOLT_PROXY_PASSWORD")
	certFile = os.Getenv("BOLT_PROXY_CERT")
	keyFile = os.Getenv("BOLT_PROXY_KEY")

	// to keep it easy, let the defaults be populated by the env vars
	flag.StringVar(&proxy_params.bindOn, "bind", bindOn, "host:port to bind to")
	flag.StringVar(&proxy_params.proxyTo, "uri", proxyTo, "bolt uri for remote Memgraph")
	flag.StringVar(&proxy_params.username, "user", username, "Memgraph username")
	flag.StringVar(&proxy_params.password, "pass", password, "Memgraph password")
	flag.StringVar(&proxy_params.certFile, "cert", certFile, "x509 certificate")
	flag.StringVar(&proxy_params.keyFile, "key", keyFile, "x509 private key")
	flag.BoolVar(&proxy_params.debugMode, "debug", debugMode, "enable debug logging")
	flag.Parse()
}

func main() {
	// Set up loggers
	proxy_logger.SetUpInfoLog(os.Stdout)
	proxy_logger.SetUpWarnLog(os.Stderr)
	if proxy_params.debugMode {
		proxy_logger.SetUpDebugLog(os.Stdout)
	} else {
		proxy_logger.SetUpDebugLog(ioutil.Discard)
	}

	// ---------- pprof debugger
	// go func() {
	// 	info.Println(http.ListenAndServe("localhost:6060", nil))
	// }()

	// ---------- BACK END
	proxy_logger.InfoLog.Println("starting bolt-proxy backend")
	backend, err := backend.NewBackend(proxy_params.username, proxy_params.password, proxy_params.proxyTo)
	if err != nil {
		proxy_logger.WarnLog.Fatal(err)
	}
	proxy_logger.InfoLog.Println("connected to backend", proxy_params.proxyTo)
	proxy_logger.InfoLog.Printf("found backend version %s\n", backend.Version())

	// ---------- FRONT END
	proxy_logger.InfoLog.Println("starting bolt-proxy frontend")

	var listener net.Listener
	if proxy_params.certFile == "" || proxy_params.keyFile == "" {
		// non-tls
		listener, err = net.Listen("tcp", proxy_params.bindOn)
		if err != nil {
			proxy_logger.WarnLog.Fatal(err)
		}
		proxy_logger.InfoLog.Printf("listening on %s\n", proxy_params.bindOn)
	} else {
		// tls
		cert, err := tls.LoadX509KeyPair(proxy_params.certFile, proxy_params.keyFile)
		if err != nil {
			proxy_logger.WarnLog.Fatal(err)
		}
		config := &tls.Config{Certificates: []tls.Certificate{cert}}
		listener, err = tls.Listen("tcp", proxy_params.bindOn, config)
		if err != nil {
			proxy_logger.WarnLog.Fatal(err)
		}
		proxy_logger.InfoLog.Printf("listening for TLS connections on %s\n", proxy_params.bindOn)
	}
	// ---------- Event Loop
	for {
		conn, err := listener.Accept()
		if err != nil {
			proxy_logger.WarnLog.Printf("Error: %v\n", err)
		} else {
			go frontend.HandleClient(conn, backend)
		}
	}
}
