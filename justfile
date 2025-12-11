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
    swag fmt -g controlplane/controlplane_routes.go -d ./
    swag init --pd -g controlplane/controlplane_routes.go -ot go ./

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

# Starts a client against localhost:8080 with sensible defaults
[group('dev')]
run-client-simple user_email server_url="http://localhost:8080" base_clients_dir="~/.syftbox/clients" *ARGS="":
    #!/bin/bash
    set -eou pipefail

    CLIENT_DIR="{{ base_clients_dir }}/{{ user_email }}"
    CONFIG_PATH="${CLIENT_DIR}/config.json"
    DATA_DIR="${CLIENT_DIR}/SyftBox"

    mkdir -p "{{ base_clients_dir }}"
    mkdir -p "$CLIENT_DIR"

    echo "Running client:"
    echo "  Email: {{ user_email }}"
    echo "  Server: {{ server_url }}"
    echo "  Data dir: $DATA_DIR"
    echo "  Config path: $CONFIG_PATH"

    just run-client -e "{{ user_email }}" -s "{{ server_url }}" -d "$DATA_DIR" -c "$CONFIG_PATH" {{ ARGS }}

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

[group('dev-docker')]
run-docker-server:
    #!/bin/bash
    set -eou pipefail
    echo "Building and running SyftBox server with MinIO in Docker..."
    cd docker && COMPOSE_BAKE=true docker-compose up -d --build minio server
    echo "Server is running at http://localhost:8080"
    echo "MinIO console is available at http://localhost:9001"
    echo "Run 'cd docker && docker-compose logs -f server' to view server logs"

[group('dev-docker')]
run-docker-client email *ARGS:
    #!/bin/bash
    set -eou pipefail
    
    # Build the client image
    docker build -f docker/Dockerfile.client -t syftbox-client .
    
    # Create clients directory if it doesn't exist
    mkdir -p ~/.syftbox/clients
    
    if [ -z "{{ email }}" ]; then
        echo "Usage: just run-docker-client <email> [command]"
        echo "Examples:"
        echo "  just run-docker-client user@example.com login"
        echo "  just run-docker-client user@example.com daemon"
        echo "  just run-docker-client user@example.com app list"
        exit 1
    fi
    
    # Sanitize email for container name (replace @ with -at- and . with -dot-)
    container_name="syftbox-client-$(echo '{{ email }}' | sed 's/@/-at-/g' | sed 's/\./-dot-/g')"
    
    # Run the client with email-specific configuration
    docker run --rm -it \
      -v ~/.syftbox/clients:/data/clients \
      --network docker_syftbox-network \
      -e SYFTBOX_SERVER_URL=http://syftbox-server:8080 \
      -e SYFTBOX_AUTH_ENABLED=0 \
      --name "$container_name" \
      syftbox-client {{ email }} {{ ARGS }}

[group('dev-docker')]
run-docker-client-daemon email:
    #!/bin/bash
    set -eou pipefail
    
    # Build and run client in daemon mode using docker-compose
    cd docker && CLIENT_EMAIL={{ email }} docker-compose -f docker-compose-client.yml up -d --build
    echo "Client daemon for {{ email }} is running at http://localhost:7938"
    echo "Logs: cd docker && docker-compose -f docker-compose-client.yml logs -f"

[group('dev-docker')]
stop-docker-client email:
    #!/bin/bash
    set -eou pipefail
    
    cd docker && CLIENT_EMAIL={{ email }} docker-compose -f docker-compose-client.yml down

[group('dev-docker')]
list-docker-clients:
    #!/bin/bash
    set -eou pipefail
    
    echo "Available SyftBox clients:"
    if [ -d ~/.syftbox/clients ]; then
        ls -la ~/.syftbox/clients/ | grep -E '^d' | grep -v '\.$' | awk '{print "  - " $NF}'
    else
        echo "  No clients found"
    fi

[group('dev-docker')]
destroy-docker-server:
    #!/bin/bash
    set -eou pipefail
    echo "Stopping and removing SyftBox Docker containers..."
    cd docker && docker-compose down -v
    echo "Removing Docker images..."
    docker rmi syftbox-server syftbox-client 2>/dev/null || true
    echo "Docker environment cleaned up"

[group('devstack')]
sbdev-start *ARGS:
    GOCACHE=$(pwd)/.gocache go run ./cmd/devstack start {{ ARGS }}

[group('devstack')]
sbdev-stop *ARGS:
    GOCACHE=$(pwd)/.gocache go run ./cmd/devstack stop {{ ARGS }}

[group('devstack')]
sbdev-status *ARGS:
    GOCACHE=$(pwd)/.gocache go run ./cmd/devstack status {{ ARGS }}

[group('devstack')]
sbdev-logs *ARGS:
    GOCACHE=$(pwd)/.gocache go run ./cmd/devstack logs {{ ARGS }}

[group('devstack')]
sbdev-nuke:
    #!/bin/bash
    set -euo pipefail
    echo "Killing all syftbox clients/servers/minio processes..."
    # match patterns but avoid killing current shell
    pids=$(ps -eo pid,comm,args | grep -E 'syftbox|/minio|/server' | grep -v 'grep' | awk '{print $1}')
    if [ -z "$pids" ]; then
        echo "No matching processes found."
    else
        echo "$pids" | xargs -r kill -9
        echo "Killed PIDs: $pids"
    fi
    echo "Removing sandbox directory if it exists..."
    rm -rf sandbox
    echo "Nuke complete."

[group('devstack')]
sbdev-test-perf *ARGS:
    #!/bin/bash
    set -eou pipefail
    echo "Running devstack performance tests..."
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        SANDBOX_DIR="$REPO_ROOT/.test-sandbox/perf-tests"
    fi
    echo "Using sandbox: $SANDBOX_DIR"
    rm -rf "$SANDBOX_DIR"
    PERF_TEST_SANDBOX="$SANDBOX_DIR" go test -v -timeout 30m -tags integration {{ ARGS }}
    echo "Test artifacts preserved at: $SANDBOX_DIR"

[group('devstack')]
sbdev-test-perf-profile *ARGS:
    #!/bin/bash
    set -eou pipefail
    echo "Running performance tests with profiling enabled..."
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        SANDBOX_DIR="$REPO_ROOT/.test-sandbox/perf-tests-profile"
    fi
    echo "Using sandbox: $SANDBOX_DIR"
    rm -rf "$SANDBOX_DIR"
    PERF_TEST_SANDBOX="$SANDBOX_DIR" PERF_PROFILE=1 go test -v -timeout 30m -tags integration {{ ARGS }}
    echo ""
    echo "Profiles saved to: cmd/devstack/profiles/"
    echo "View flame graphs: go tool pprof -http=:8080 cmd/devstack/profiles/{test}/cpu.prof"

[group('devstack')]
sbdev-test-perf-sandbox *ARGS:
    #!/bin/bash
    set -eou pipefail
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) sandbox_path="$PERF_TEST_SANDBOX" ;;
            *) sandbox_path="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        sandbox_path="$REPO_ROOT/.test-sandbox/perf-sandbox"
    fi
    echo "Running performance tests with persistent sandbox: $sandbox_path"
    rm -rf "$sandbox_path"
    cd cmd/devstack
    PERF_TEST_SANDBOX="$sandbox_path" go test -v -timeout 30m -tags integration {{ ARGS }}
    echo ""
    echo "Test files preserved in: $sandbox_path"
    echo "Files from alice: $sandbox_path/alice@example.com/datasites/datasites/alice@example.com/public/"
    echo "Files synced to bob: $sandbox_path/bob@example.com/datasites/datasites/alice@example.com/public/"

[group('devstack')]
sbdev-list:
    GOCACHE=$(pwd)/.gocache go run ./cmd/devstack list

[group('devstack')]
sbdev-prune:
    GOCACHE=$(pwd)/.gocache go run ./cmd/devstack prune

[group('devstack')]
sbdev-test-cleanup:
    #!/bin/bash
    set -euo pipefail
    echo "Cleaning up test sandbox..."
    cd cmd/devstack
    go run . stop --path ../../sandbox 2>/dev/null || echo "Test sandbox not running"

[group('devstack')]
sbdev-test-all:
    #!/bin/bash
    set -eou pipefail
    echo "Running all performance tests..."
    echo ""
    just sbdev-test-acl
    echo ""
    just sbdev-test-ws
    echo ""
    just sbdev-test-large
    echo ""
    just sbdev-test-concurrent
    echo ""
    just sbdev-test-conflict
    echo ""
    just sbdev-test-many
    echo ""
    just sbdev-test-ack
    echo ""
    echo "✅ All performance tests completed!"

[group('devstack')]
sbdev-test-acl:
    #!/bin/bash
    set -eou pipefail
    RUNS=${1:-1}
    shift || true
    echo "Running ACL race condition test ($RUNS time(s))..."
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    # Resolve sandbox base path
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) BASE_SANDBOX="$PERF_TEST_SANDBOX" ;;
            *)  BASE_SANDBOX="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        BASE_SANDBOX="$REPO_ROOT/.test-sandbox/acl-test"
    fi

    for i in $(seq 1 "$RUNS"); do
        if [ "$RUNS" -gt 1 ]; then
            SANDBOX_DIR="${BASE_SANDBOX}-${i}"
            echo "Run $i/$RUNS using sandbox: $SANDBOX_DIR"
        else
            SANDBOX_DIR="$BASE_SANDBOX"
            echo "Using sandbox: $SANDBOX_DIR"
        fi
        rm -rf "$SANDBOX_DIR"
        PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 10m -tags integration -run TestACLRaceCondition "$@"
        echo "Test artifacts preserved at: $SANDBOX_DIR"
    done

[group('devstack')]
sbdev-test-acl-prop:
    #!/bin/bash
    set -eou pipefail
    RUNS=${1:-1}
    shift || true
    echo "Running ACL propagation regression test ($RUNS time(s))..."
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    # Resolve sandbox base path
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) BASE_SANDBOX="$PERF_TEST_SANDBOX" ;;
            *)  BASE_SANDBOX="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        BASE_SANDBOX="$REPO_ROOT/.test-sandbox/acl-prop"
    fi

    for i in $(seq 1 "$RUNS"); do
        if [ "$RUNS" -gt 1 ]; then
            SANDBOX_DIR="${BASE_SANDBOX}-${i}"
            echo "Run $i/$RUNS using sandbox: $SANDBOX_DIR"
        else
            SANDBOX_DIR="$BASE_SANDBOX"
            echo "Using sandbox: $SANDBOX_DIR"
        fi
        rm -rf "$SANDBOX_DIR"
        PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 10m -tags integration -run TestACLPropagationUpdates "$@"
        echo "Test artifacts preserved at: $SANDBOX_DIR"
    done

[group('devstack')]
sbdev-test-ws:
    #!/bin/bash
    set -eou pipefail
    echo "Running WebSocket latency test..."
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        SANDBOX_DIR="$REPO_ROOT/.test-sandbox/ws-test"
    fi
    rm -rf "$SANDBOX_DIR"
    PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 10m -tags integration -run TestWebSocketLatency
    echo "Test artifacts preserved at: $SANDBOX_DIR"

[group('devstack')]
sbdev-test-large:
    #!/bin/bash
    set -eou pipefail
    echo "Running large file transfer test..."
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        SANDBOX_DIR="$REPO_ROOT/.test-sandbox/large-test"
    fi
    rm -rf "$SANDBOX_DIR"
    PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -v -timeout 30m -tags integration -run TestLargeFileTransfer

[group('devstack')]
sbdev-test-large-upload:
    #!/bin/bash
    set -eou pipefail
    echo "Running resumable large upload test..."
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        SANDBOX_DIR="$REPO_ROOT/.test-sandbox/large-upload"
    fi
    rm -rf "$SANDBOX_DIR"
    PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -v -timeout 60m -tags integration -run TestLargeUploadResume

[group('devstack')]
sbdev-test-progress-api:
    #!/bin/bash
    set -eou pipefail
    echo "Running Progress API demo..."
    echo "This demo shows the sync status and upload management APIs in action."
    echo "Features: status tracking, progress bars, pause/resume, error handling, auth"
    echo ""
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        SANDBOX_DIR="$REPO_ROOT/.test-sandbox/progress-api-demo"
    fi
    rm -rf "$SANDBOX_DIR"
    PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -v -timeout 15m -tags integration -run TestProgressAPIDemo

[group('devstack')]
sbdev-test-concurrent:
    #!/bin/bash
    set -eou pipefail
    echo "Running concurrent upload test..."
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        SANDBOX_DIR="$REPO_ROOT/.test-sandbox/concurrent-test"
    fi
    rm -rf "$SANDBOX_DIR"
    PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -v -timeout 15m -tags integration -run TestConcurrentUploads

[group('devstack')]
sbdev-test-conflict:
    #!/bin/bash
    set -eou pipefail
    echo "Running conflict resolution tests (simultaneous writes, divergent edits, etc.)..."
    RUNS=${1:-1}
    shift || true
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) BASE_SANDBOX="$PERF_TEST_SANDBOX" ;;
            *) BASE_SANDBOX="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        BASE_SANDBOX="$REPO_ROOT/.test-sandbox/conflict-test"
    fi

    # Run each test separately to prevent state pollution
    TESTS=("TestSimultaneousWrite" "TestDivergentEdits" "TestThreeWayConflict" "TestConflictDuringACLChange" "TestNestedPathConflict" "TestJournalWriteTiming" "TestNonConflictUpdate" "TestRapidSequentialEdits" "TestJournalLossRecovery")
    for i in $(seq 1 "$RUNS"); do
        if [ "$RUNS" -gt 1 ]; then
            SANDBOX_DIR="${BASE_SANDBOX}-${i}"
            echo "Run $i/$RUNS using sandbox: $SANDBOX_DIR"
        else
            SANDBOX_DIR="$BASE_SANDBOX"
            echo "Using sandbox: $SANDBOX_DIR"
        fi

        for TEST in "${TESTS[@]}"; do
            echo "Running $TEST..."
            rm -rf "$SANDBOX_DIR"
            PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 10m -tags integration -run "^${TEST}$" "$@" || exit 1
        done
        echo "Test artifacts preserved at: $SANDBOX_DIR"
    done

[group('devstack')]
sbdev-test-many:
    #!/bin/bash
    set -eou pipefail
    echo "Running many small files test..."
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        SANDBOX_DIR="$REPO_ROOT/.test-sandbox/batch-test"
    fi
    rm -rf "$SANDBOX_DIR"
    PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 15m -tags integration -run TestManySmallFiles
    echo "Test artifacts preserved at: $SANDBOX_DIR"

sbdev-test-ack:
    #!/bin/bash
    set -eou pipefail
    echo "Running ACK/NACK mechanism test..."
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        SANDBOX_DIR="$REPO_ROOT/.test-sandbox/ack-test"
    fi
    rm -rf "$SANDBOX_DIR"
    PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 5m -tags integration -run TestACKNACKMechanism
    echo "Test artifacts preserved at: $SANDBOX_DIR"

sbdev-test-profile:
    #!/bin/bash
    set -eou pipefail
    echo "Running performance profiling test with CPU/memory tracking..."
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        SANDBOX_DIR="$REPO_ROOT/.test-sandbox/profile-test"
    fi
    rm -rf "$SANDBOX_DIR"
    PERF_TEST_SANDBOX="$SANDBOX_DIR" PERF_PROFILE=1 GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 10m -tags integration -run TestProfilePerformance
    echo ""
    echo "✅ Profile data saved to: cmd/devstack/profiles/performance_profile/"
    echo "   - cpu.prof (CPU profile)"
    echo "   - trace.out (execution trace)"
    echo "   - mem.prof (memory profile)"
    echo ""
    echo "Generate flame graphs with: just sbdev-flamegraph"

sbdev-test-race:
    #!/bin/bash
    set -eou pipefail
    echo "Running race condition tests (delete during download, ACL during upload, etc.)..."
    RUNS=${1:-1}
    shift || true
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) BASE_SANDBOX="$PERF_TEST_SANDBOX" ;;
            *) BASE_SANDBOX="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        BASE_SANDBOX="$REPO_ROOT/.test-sandbox/race-test"
    fi

    # Run each test separately to prevent state pollution
    TESTS=("TestDeleteDuringDownload" "TestACLChangeDuringUpload" "TestOverwriteDuringDownload" "TestDeleteDuringTempRename")
    for i in $(seq 1 "$RUNS"); do
        if [ "$RUNS" -gt 1 ]; then
            SANDBOX_DIR="${BASE_SANDBOX}-${i}"
            echo "Run $i/$RUNS using sandbox: $SANDBOX_DIR"
        else
            SANDBOX_DIR="$BASE_SANDBOX"
            echo "Using sandbox: $SANDBOX_DIR"
        fi

        for TEST in "${TESTS[@]}"; do
            echo "Running $TEST..."
            rm -rf "$SANDBOX_DIR"
            PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 10m -tags integration -run "^${TEST}$" "$@" || exit 1
        done
        echo "Test artifacts preserved at: $SANDBOX_DIR"
    done

sbdev-test-chaos:
    #!/bin/bash
    set -eou pipefail
    echo "Running chaos sync test (3 clients, random ops)..."
    RUNS=${1:-1}
    shift || true
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) BASE_SANDBOX="$PERF_TEST_SANDBOX" ;;
            *) BASE_SANDBOX="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        BASE_SANDBOX="$REPO_ROOT/.test-sandbox/chaos-test"
    fi
    for i in $(seq 1 "$RUNS"); do
        if [ "$RUNS" -gt 1 ]; then
            SANDBOX_DIR="${BASE_SANDBOX}-${i}"
            echo "Run $i/$RUNS using sandbox: $SANDBOX_DIR"
        else
            SANDBOX_DIR="$BASE_SANDBOX"
            echo "Using sandbox: $SANDBOX_DIR"
        fi
        rm -rf "$SANDBOX_DIR"
        PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 10m -tags integration -run TestChaosSync "$@"
        echo "Test artifacts preserved at: $SANDBOX_DIR"
    done

sbdev-flamegraph:
    #!/bin/bash
    set -eou pipefail
    cd cmd/devstack/profiles/performance_profile
    echo "Generating flame graphs from profiling data..."
    echo ""
    echo "🔥 CPU Flame Graph:"
    echo "   go tool pprof -http=:8080 cpu.prof"
    echo ""
    echo "🔥 Memory Flame Graph:"
    echo "   go tool pprof -http=:8081 mem.prof"
    echo ""
    echo "🔥 Execution Trace (detailed timeline):"
    echo "   go tool trace trace.out"
    echo ""
    echo "Interactive CPU profile analysis starting..."
    go tool pprof -http=:8080 cpu.prof

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
build-client-target goos=`go env GOOS` goarch=`go env GOARCH`: version-utils
    #!/bin/bash
    set -eou pipefail

    # Calculate build variables locally
    SYFTBOX_VERSION=$(svu current 2>/dev/null)
    echo "SYFTBOX_VERSION: $SYFTBOX_VERSION"
    BUILD_COMMIT=$(git rev-parse --short HEAD)
    BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    BUILD_LD_FLAGS="-s -w -X github.com/openmined/syftbox/internal/version.Version=$SYFTBOX_VERSION -X github.com/openmined/syftbox/internal/version.Revision=$BUILD_COMMIT -X github.com/openmined/syftbox/internal/version.BuildDate=$BUILD_DATE"

    export GOOS="{{ goos }}"
    export GOARCH="{{ goarch }}"
    export CGO_ENABLED=0
    export GO_LDFLAGS="$([ '{{ goos }}' = 'windows' ] && echo '-H windowsgui ')$BUILD_LD_FLAGS"

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
deploy-client remote: build-all
    #!/bin/bash
    echo "Deploying syftbox client to {{ _cyan }}{{ remote }}{{ _nc }}"
    
    rm -rf releases && mkdir releases
    cp -r .out/syftbox_client_*.{tar.gz,zip} releases/
    ssh {{ remote }} "rm -rfv /home/azureuser/releases.new && mkdir -p /home/azureuser/releases.new"
    scp -r ./releases/* {{ remote }}:/home/azureuser/releases.new/
    ssh {{ remote }} "rm -rfv /home/azureuser/releases/ && mv -fv /home/azureuser/releases.new/ /home/azureuser/releases/"

[group('deploy')]
deploy-server remote: build-server
    #!/bin/bash
    echo "Deploying syftbox server to {{ _cyan }}{{ remote }}{{ _nc }}"

    scp .out/syftbox_server_linux_amd64_v1/syftbox_server {{ remote }}:/home/azureuser/syftbox_server_new
    ssh {{ remote }} "rm -fv /home/azureuser/syftbox_server && mv -fv /home/azureuser/syftbox_server_new /home/azureuser/syftbox_server"
    ssh {{ remote }} "sudo systemctl restart syftbox"

[group('deploy')]
deploy remote: (deploy-client remote) (deploy-server remote)
    echo "Deployed syftbox client & server to {{ _cyan }}{{ remote }}{{ _nc }}"

[group('utils')]
setup-toolchain:
    go install github.com/swaggo/swag/v2/cmd/swag@latest
    go install github.com/bokwoon95/wgo@latest
    go install filippo.io/mkcert@latest

[group('utils')]
email-hash email domain="":
    #!/bin/bash
    set -eou pipefail
    
    if [ -z "{{ email }}" ]; then
        echo "Usage: just email-hash <email> [domain]"
        echo "Examples:"
        echo "  just email-hash alice@example.com"
        echo "  just email-hash alice@example.com syftbox.com"
        exit 1
    fi
    
    # Generate the hash (first 16 chars of sha256)
    hash=$(echo -n "{{ email }}" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]' | shasum -a 256 | cut -c1-16)
    
    echo "Email: {{ email }}"
    echo "Hash: $hash"
    echo ""
    
    if [ -z "{{ domain }}" ]; then
        # No domain provided, use local development URL
        echo "URL: http://$hash.syftbox.local:8080/"
    else
        # Domain provided, use HTTPS
        echo "URL: https://$hash.{{ domain }}/"
    fi

[group('utils')]
clean:
    rm -rf .data .out releases certs cover.out

[group('version')]
bump type: version-utils
    #!/bin/bash
    set -eou pipefail

    # Version Management Commands
    #
    # This project uses semantic versioning with svu (https://github.com/caarlos0/svu)
    # for automatic version calculation based on git tags.
    #
    # Workflow:
    # 1. Use `just show-version` to see current version and next versions
    # 2. Use `just bump type` to update files only (manual commit/tag)
    # 3. Use `just release type` to update files, commit, and tag automatically
    # 4. The version.go file is updated automatically with the new version 
    #    from the git tag using the goreleaser.yaml file.

    # Examples:
    #   just show-version                    # Show current and next versions
    #   just bump patch                      # Update files to next patch version
    #   just bump minor                      # Update files to next minor version
    #   just bump major                      # Update files to next major version
    #   just release patch                   # Bump, commit, and tag patch version
    
    if [ -z "{{ type }}" ]; then
        echo -e "{{ _red }}Error: bump type is required{{ _nc }}"
        echo "Usage: just bump <patch|minor|major>"
        echo "Examples:"
        echo "  just bump patch"
        echo "  just bump minor"
        echo "  just bump major"
        exit 1
    fi
    
    # Validate bump type
    if [[ ! "{{ type }}" =~ ^(patch|minor|major)$ ]]; then
        echo -e "{{ _red }}Error: Invalid bump type '{{ type }}'{{ _nc }}"
        echo "Valid types: patch, minor, major"
        exit 1
    fi
    
    echo -e "{{ _cyan }}Bumping {{ type }} version...{{ _nc }}"
    new_version=$(svu {{ type }} | sed 's/^v//')
    echo -e "{{ _green }}New version: $new_version{{ _nc }}"
    echo -e "{{ _green }}Version bumped to $new_version{{ _nc }}"
    echo -e "{{ _yellow }}Don't forget to commit and tag:{{ _nc }}"
    echo "  git add ."
    echo "  git commit -m \"chore: bump version to $new_version\""
    echo "  git tag v$new_version"

release type: version-utils
    #!/bin/bash
    set -eou pipefail
    
    if [ -z "{{ type }}" ]; then
        echo -e "{{ _red }}Error: release type is required{{ _nc }}"
        echo "Usage: just release <patch|minor|major>"
        echo "Examples:"
        echo "  just release patch"
        echo "  just release minor"
        echo "  just release major"
        exit 1
    fi
    
    # Validate release type
    if [[ ! "{{ type }}" =~ ^(patch|minor|major)$ ]]; then
        echo -e "{{ _red }}Error: Invalid release type '{{ type }}'{{ _nc }}"
        echo "Valid types: patch, minor, major"
        exit 1
    fi
    
    echo -e "{{ _cyan }}Releasing {{ type }} version...{{ _nc }}"
    new_version=$(svu {{ type }} | sed 's/^v//')
    echo -e "{{ _green }}New version: $new_version{{ _nc }}"
    just commit-and-tag version="$new_version"
    echo -e "{{ _green }}✓ Released {{ type }} version $new_version{{ _nc }}"

[group('version')]
show-version: version-utils
    #!/bin/bash
    set -eou pipefail
    echo -e "{{ _cyan }}Current version information:{{ _nc }}"
    
    # Try to get current version, handle errors gracefully
    current_version=$(svu current 2>/dev/null || echo "No valid version tags found")
    echo "  SVU current: $current_version"
    
    # Try to get next versions, handle errors gracefully
    next_patch=$(svu patch 2>/dev/null || echo "Error")
    next_minor=$(svu minor 2>/dev/null || echo "Error")
    next_major=$(svu major 2>/dev/null || echo "Error")
    
    echo "  SVU next patch: $next_patch"
    echo "  SVU next minor: $next_minor"
    echo "  SVU next major: $next_major"
    echo "  Git tags:"
    git tag --sort=-version:refname | head -5

[group('version')]
commit-and-tag version:
    #!/bin/bash
    set -eou pipefail
    
    # Extract version from parameter (handle both "version=0.5.1" and "0.5.1" formats)
    version_value="{{ version }}"
    if [[ "$version_value" == version=* ]]; then
        version_value="${version_value#version=}"
    fi
    
    if [ -z "$version_value" ]; then
        echo -e "{{ _red }}Error: version parameter is required{{ _nc }}"
        echo "Usage: just commit-and-tag version=1.2.3"
        exit 1
    fi
    
    echo -e "{{ _cyan }}Committing and tagging version $version_value...{{ _nc }}"
    
    # Check if there are changes to commit
    if git diff --quiet && git diff --cached --quiet; then
        echo -e "{{ _yellow }}No changes to commit{{ _nc }}"
    else
        git add .
        git commit -m "chore: bump version to $version_value"
        echo -e "{{ _green }}✓ Committed changes{{ _nc }}"
    fi
    
    # Check if tag already exists and force update it
    if git tag -l "v$version_value" | grep -q "v$version_value"; then
        echo -e "{{ _yellow }}Tag v$version_value already exists. Force updating...{{ _nc }}"
        git tag -f v$version_value
        echo -e "{{ _green }}✓ Force updated tag v$version_value{{ _nc }}"
    else
        # Create new tag
        git tag v$version_value
        echo -e "{{ _green }}✓ Created tag v$version_value{{ _nc }}"
    fi
    
    echo -e "{{ _green }}Version $version_value has been committed and tagged!{{ _nc }}"

[group('version')]
version-utils:
    go install github.com/caarlos0/svu@latest
