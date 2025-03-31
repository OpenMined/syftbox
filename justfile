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
    goreleaser release --snapshot --clean --id syftbox_client

[group('build')]
build-server:
    goreleaser release --snapshot --clean --id syftbox_server

[group('build')]
build-all: 
    goreleaser release --snapshot --clean

[group('deploy')]
deploy: build-all
    rm -rf releases && mkdir releases
    cp -r .out/syftbox_client*.tar.gz releases

    ssh syftbox-yash "rm -rfv /home/azureuser/releases"
    scp -r ./releases syftbox-yash:/home/azureuser/releases

    ssh syftbox-yash "rm -fv /home/azureuser/syftbox_server"
    scp .out/syftbox_server_linux_amd64_v1/syftbox_server syftbox-yash:/home/azureuser/syftbox_server
    ssh syftbox-yash "sudo systemctl restart syftgo"

    rm -rf releases
