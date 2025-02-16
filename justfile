
GOX := `echo $(go env GOPATH)/bin/gox`

APP_NAME := "syftgo"
TARGET_DIR := ".out"
GO_LDFLAGS := "-s -w"
GOX_ARCH := "amd64 arm64"
GOX_OS := "darwin linux windows"
GOX_OUT := TARGET_DIR + "/" + APP_NAME + "_{{.OS}}_{{.Arch}}"


SYFTBOX_ENTRYPOINT := "./cmd/server"

gen-certs:
    #!/bin/bash
    # if certs.key and certs.pem exist, exit\
    if [ -f certs/cert.key ] && [ -f certs/cert.pem ]; then
        exit 0;
    fi
    mkdir certs
    mkcert -install -cert-file certs/cert.pem -key-file certs/cert.key localhost 127.0.0.1

run-server: gen-certs
    go run ./cmd/server --cert certs/cert.pem --key certs/cert.key


clear-builds:
    rm -rf {{ TARGET_DIR }}

build-all: clear-builds
    {{ GOX }} -arch "{{ GOX_ARCH }}" -os "{{ GOX_OS }}" -ldflags "{{ GO_LDFLAGS }}" -output "{{ GOX_OUT }}" {{ SYFTBOX_ENTRYPOINT }}

install-gox:
    go install github.com/mitchellh/gox@latest
