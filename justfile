GOX := `echo $(go env GOPATH)/bin/gox`

APP_NAME := "syft"
TARGET_DIR := ".out"
GO_LDFLAGS := "-s -w"
GOX_ARCH := "amd64 arm64"
GOX_OS := "darwin linux"
GOX_OUT := TARGET_DIR + "/" + APP_NAME + "_{{.Dir}}_{{.OS}}_{{.Arch}}"

default:
    just --list

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
    {{ GOX }} -arch "{{ GOX_ARCH }}" -os "{{ GOX_OS }}" -ldflags "{{ GO_LDFLAGS }}" -output "{{ GOX_OUT }}" ./cmd/server ./cmd/client

codesign:
    codesign --verbose --force --deep --verify --timestamp --sign "Developer ID Application: OpenMined Foundation (28PJ5N8D9X)" .out/syft_server_darwin_arm64 .out/syft_server_darwin_amd64
    codesign -dv --verbose=4 .out/syft_server_darwin_arm64
    codesign -dv --verbose=4 .out/syft_server_darwin_amd64

install-gox:
    go install github.com/mitchellh/gox@latest
