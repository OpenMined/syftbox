SYFTBOX_VERSION := "0.5.0"
BUILD_COMMIT := `git rev-parse --short HEAD`
BUILD_DATE := `date -u +%Y-%m-%dT%H:%M:%SZ`
BUILD_LD_FLAGS := "-s -w" + " -X github.com/openmined/syftbox/internal/version.Version=" + SYFTBOX_VERSION + " -X github.com/openmined/syftbox/internal/version.Revision=" + BUILD_COMMIT + " -X github.com/openmined/syftbox/internal/version.BuildDate=" + BUILD_DATE
CLIENT_BUILD_TAGS := "go_json nomsgpack"
SERVER_BUILD_TAGS := "sonic avx nomsgpack"

_red := '\033[1;31m'
_cyan := '\033[1;36m'
_green := '\033[1;32m'
_yellow := '\033[1;33m'
_nc := '\033[0m'

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
    swag fmt -g controlplane_routes.go -d ./
    swag init --pd -g controlplane_routes.go -ot go ./

[group('dev')]
run-server *ARGS:
    go run -tags="{{ SERVER_BUILD_TAGS }}" ./cmd/server {{ ARGS }}

[group('dev')]
run-server-tls *ARGS: gen-certs
    go run -tags="{{ SERVER_BUILD_TAGS }}" ./cmd/server --cert certs/cert.pem --key certs/cert.key {{ ARGS }}

[group('dev')]
run-server-reload *ARGS:
    wgo run -file 'cmd/.*' -file 'internal/.*' -file 'config/.*' -tags="{{ SERVER_BUILD_TAGS }}" ./cmd/server {{ ARGS }}

[group('dev')]
run-client *ARGS: gen-swagger
    go run -tags="{{ CLIENT_BUILD_TAGS }}" ./cmd/client {{ ARGS }}

[group('dev')]
run-client-reload *ARGS:
    wgo run -file 'cmd/.*' -file 'internal/.*' -tags="{{ CLIENT_BUILD_TAGS }}" ./cmd/client {{ ARGS }}

[group('dev-minio')]
run-minio:
    #!/bin/bash
    set -eou pipefail

    docker rm -f syftbox-minio || true
    docker run -d \
      --name syftbox-minio \
      -p 9000:9000 \
      -p 9001:9001 \
      -e MINIO_ROOT_USER=minioadmin \
      -e MINIO_ROOT_PASSWORD=minioadmin \
      -v minio-data:/data \
      -v $(pwd)/minio/init.d:/etc/minio/init.d \
      minio/minio:RELEASE.2025-04-22T22-12-26Z server /data --console-address ':9001'

    until docker exec syftbox-minio sh -c "mc --version" >/dev/null 2>&1; do
      sleep 1
    done

    docker exec syftbox-minio /etc/minio/init.d/setup.sh

[group('dev-minio')]
destroy-minio:
    docker rm -f syftbox-minio && docker volume rm minio-data || true

[group('dev-minio')]
ssh-minio:
    docker exec -it syftbox-minio bash

[group('dev')]
test:
    env -i \
        PATH="$PATH" \
        HOME="$HOME" \
        GOROOT="${GOROOT:-}" \
        GOPATH="${GOPATH:-}" \
        GOCACHE="${GOCACHE:-}" \
        GOENV="${GOENV:-}" \
        go test -coverprofile=cover.out ./...
    go tool cover -html=cover.out


[doc('Needs a platform specific compiler. Example: CC="aarch64-linux-musl-gcc" just build-client-target goos=linux goarch=arm64')]
[group('build')]
build-client-target goos=`go env GOOS` goarch=`go env GOARCH`:
    #!/bin/bash
    set -eou pipefail

    export GOOS="{{ goos }}"
    export GOARCH="{{ goarch }}"
    export CGO_ENABLED=0
    export GO_LDFLAGS="$([ '{{ goos }}' = 'windows' ] && echo '-H windowsgui '){{ BUILD_LD_FLAGS }}"

    if [ "{{ goos }}" = "darwin" ]; then
        echo "Building for darwin. CGO_ENABLED=1"
        export CGO_ENABLED=1
    fi

    rm -rf .out && mkdir -p .out
    go build -x -trimpath --tags="{{ CLIENT_BUILD_TAGS }}" \
        -ldflags="$GO_LDFLAGS" \
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
deploy-client remote="syftbox-yash": build-all
    echo "Deploying syftbox client to {{ _cyan }}{{ remote }}{{ _nc }}"
    rm -rf releases && mkdir releases
    cp -r .out/syftbox_client_*.{tar.gz,zip} releases/
    ssh {{ remote }} "rm -rfv /home/azureuser/releases.new && mkdir -p /home/azureuser/releases.new"
    scp -r ./releases/* {{ remote }}:/home/azureuser/releases.new/
    ssh {{ remote }} "rm -rfv /home/azureuser/releases/ && mv -fv /home/azureuser/releases.new/ /home/azureuser/releases/"

[group('deploy')]
deploy-server remote="syftbox-yash": build-server
    echo "Deploying syftbox server to {{ _cyan }}{{ remote }}{{ _nc }}"
    scp .out/syftbox_server_linux_amd64_v1/syftbox_server {{ remote }}:/home/azureuser/syftbox_server_new
    ssh {{ remote }} "rm -fv /home/azureuser/syftbox_server && mv -fv /home/azureuser/syftbox_server_new /home/azureuser/syftbox_server"
    ssh {{ remote }} "sudo systemctl restart syftbox"

[group('deploy')]
deploy remote="syftbox-yash": (deploy-client remote) (deploy-server remote)
    echo "Deployed syftbox client & server to {{ _cyan }}{{ remote }}{{ _nc }}"

[group('utils')]
setup-toolchain:
    go install github.com/swaggo/swag/v2/cmd/swag@latest
    go install github.com/bokwoon95/wgo@latest
    go install filippo.io/mkcert@latest

[group('utils')]
clean:
    rm -rf .data .out releases certs cover.out
