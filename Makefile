.PHONY: test clean certs

KEYGEN = openssl req -x509 -newkey rsa:4096 -keyout key.pem \
		-out cert.pem -days 30 -nodes -subj '/CN=localhost'

build: clean
	go build -o bolt-proxy proxy.go

test:
	go test ./...

clean:
	go clean

certs: cert.pem key.pem
cert.pem:
	$(KEYGEN)
key.pem:
	$(KEYGEN)
