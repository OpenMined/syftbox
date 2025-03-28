GOX := `echo $(go env GOPATH)/bin/gox`


default:
    just --list

[group('dev')]
gen-certs:
    #!/bin/bash
    # if certs.key and certs.pem exist, exit\
    if [ -f certs/cert.key ] && [ -f certs/cert.pem ]; then
        exit 0;
    fi
    mkdir certs
    mkcert -install -cert-file certs/cert.pem -key-file certs/cert.key localhost 127.0.0.1

[group('dev')]
run-server: gen-certs
    go run -tags="sonic avx" ./cmd/server --cert certs/cert.pem --key certs/cert.key

[group('build')]
build-client:
    goreleaser build --snapshot --clean --id syftgo_client

[group('build')]
build-server:
    goreleaser build --snapshot --clean --id syftgo_server

[group('build')]
build-all: 
    goreleaser build --snapshot --clean

[group('deploy')]
deploy: build-server
    ssh syftbox-yash rm /home/azureuser/syft_server_linux_amd64
    scp .out/syftgo_server_linux_amd64_v1/syftgo_server syftbox-yash:/home/azureuser/syft_server_linux_amd64
    ssh syftbox-yash "sudo systemctl restart syftgo"
