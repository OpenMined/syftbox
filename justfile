SYFTBOX_VERSION := "0.5.0"
BUILD_COMMIT := `git rev-parse --short HEAD`
BUILD_DATE := `date -u +%Y-%m-%dT%H:%M:%SZ`
BUILD_LD_FLAGS := "-s -w" + " -X github.com/openmined/syftbox/internal/version.Version=" + SYFTBOX_VERSION + " -X github.com/openmined/syftbox/internal/version.Revision=" + BUILD_COMMIT + " -X github.com/openmined/syftbox/internal/version.BuildDate=" + BUILD_DATE
BUILD_TAGS := "sonic avx sqlite_omit_load_extension"

default:
    just --list

[group('dev')]
gen-certs:
    #!/bin/bash
    set -eou pipefail
    # if certs.key and certs.pem exist, exit\
    if [ -f certs/cert.key ] && [ -f certs/cert.pem ]; then
        exit 0;
    fi
    mkdir certs
    mkcert -install -cert-file certs/cert.pem -key-file certs/cert.key localhost 127.0.0.1

[group('dev')]
gen-swagger:
    #!/bin/bash
    set -eou pipefail
    cd internal/client
    swag fmt -g -d ./
    swag init -g controlplane_routes.go -o docs -ot go

[group('dev')]
run-server *ARGS: gen-swagger
    go run -tags="{{ BUILD_TAGS }}" ./cmd/server {{ ARGS }}

[group('dev')]
run-server-tls *ARGS: gen-certs gen-swagger
    go run -tags="{{ BUILD_TAGS }}" ./cmd/server --cert certs/cert.pem --key certs/cert.key {{ ARGS }}

[group('dev')]
run-client *ARGS: gen-swagger
    go run -tags="{{ BUILD_TAGS }}" ./cmd/client {{ ARGS }}

[group('dev-minio')]
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

[group('dev-minio')]
destroy-minio:
    docker rm -f syftbox-minio && docker volume rm minio-data || true

[group('dev-minio')]
ssh-minio:
    docker exec -it syftbox-minio bash

[group('dev')]
test:
    go test -coverprofile=cover.out ./...
    go tool cover -html=cover.out

[doc('Needs a platform specific compiler. Example: CC="aarch64-linux-musl-gcc" just build-client-target goos=linux goarch=arm64')]
[group('build')]
build-client-target goos=`go env GOOS` goarch=`go env GOARCH`:
    rm -rf .out && mkdir -p .out
    CGO_ENABLED=1 GOOS={{ goos }} GOARCH={{ goarch }} \
    go build -x -trimpath --tags="{{ BUILD_TAGS }}" \
        -ldflags="{{ BUILD_LD_FLAGS }}" \
        -o .out/syftbox_client_{{ goos }}_{{ goarch }} ./cmd/client

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
deploy-server: build-server
    ssh syftbox-yash "rm -fv /home/azureuser/syftbox_server"
    scp .out/syftbox_server_linux_amd64_v1/syftbox_server syftbox-yash:/home/azureuser/syftbox_server
    ssh syftbox-yash "sudo systemctl restart syftgo"

[group('deploy')]
deploy: build-all
    rm -rf releases && mkdir releases
    cp -r .out/syftbox_client_*.{tar.gz,zip} releases || true

    ssh syftbox-yash "rm -rfv /home/azureuser/releases"
    scp -r ./releases syftbox-yash:/home/azureuser/releases

    ssh syftbox-yash "rm -fv /home/azureuser/syftbox_server"
    scp .out/syftbox_server_linux_amd64_v1/syftbox_server syftbox-yash:/home/azureuser/syftbox_server
    ssh syftbox-yash "sudo systemctl restart syftgo"

    rm -rf releases

[group('utils')]
setup-toolchain:
    brew install FiloSottile/musl-cross/musl-cross
    go install github.com/air-verse/air@latest
    go install github.com/swaggo/swag/cmd/swag@latest
