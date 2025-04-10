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
run-minio:
    docker run -d \
      --name syftbox-minio \
      -p 9000:9000 \
      -p 9001:9001 \
      -e MINIO_ROOT_USER=minioadmin \
      -e MINIO_ROOT_PASSWORD=minioadmin \
      -v minio-data:/data \
      -v $(pwd)/minio/init.d:/etc/minio/init.d \
      minio/minio server /data --console-address ':9001' & \
    sleep 1 && \
    docker exec syftbox-minio /etc/minio/init.d/setup.sh

[group('dev')]
destroy-minio:
    docker rm -f syftbox-minio && docker volume rm minio-data || true

[group('dev')]
ssh-minio:
    docker exec -it syftbox-minio bash

[group('dev')]
run-server *ARGS: gen-certs
    go run -tags="sonic avx" ./cmd/server --cert certs/cert.pem --key certs/cert.key {{ARGS}}

[group('dev')]
run-client *ARGS:
    go run -tags="sonic avx" ./cmd/client {{ARGS}}

[group('dev')]
run-tests:
    go test -v -cover ./...

[group('build')]
build-client:
    goreleaser build --snapshot --clean --id syftbox_client --id syftbox_client_macos

[group('build')]
build-server:
    goreleaser build --snapshot --clean --id syftbox_server

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

[group('utils')]
setup-toolchain:
    brew install FiloSottile/musl-cross/musl-cross
