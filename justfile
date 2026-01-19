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
sbdev-start-mixed *ARGS:
    #!/bin/bash
    set -euo pipefail
    root_dir="$(pwd)"
    rust_bin="$root_dir/rust/target/release/syftbox-rs"

    go_client="alice@example.com"
    rust_client="bob@example.com"

    while [ $# -gt 0 ]; do
        case "$1" in
            --client)
                go_client="$2"
                shift 2
                ;;
            --client-rust)
                rust_client="$2"
                shift 2
                ;;
            *)
                break
                ;;
        esac
    done

    echo "Building Rust client at $rust_bin..."
    cd rust
    cargo build --release
    cd "$root_dir"

    rust_env=$(echo "$rust_client" | tr '[:lower:]' '[:upper:]' | sed 's/[^A-Z0-9]/_/g')
    env_key="SBDEV_CLIENT_BIN_${rust_env}"

    echo "Starting devstack with Rust client for $rust_client and Go client for $go_client..."
    env "$env_key=$rust_bin" GOCACHE="$root_dir/.gocache" go run ./cmd/devstack start --client "$go_client" --client "$rust_client" "$@"

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
sbdev-watch file interval="0.5":
    #!/bin/bash
    set -euo pipefail

    if [ ! -f "{{ file }}" ]; then
        echo "env file not found: {{ file }}" >&2
        echo "expected format:" >&2
        echo "  SYFTBOX_CLIENT_URL=http://127.0.0.1:PORT" >&2
        echo "  SYFTBOX_CLIENT_TOKEN=FULL_TOKEN" >&2
        exit 1
    fi

    # shellcheck disable=SC1090
    source "{{ file }}"

    if [ -z "${SYFTBOX_CLIENT_URL:-}" ] || [ -z "${SYFTBOX_CLIENT_TOKEN:-}" ]; then
        echo "env file missing SYFTBOX_CLIENT_URL or SYFTBOX_CLIENT_TOKEN" >&2
        exit 1
    fi

    echo "Watching ${SYFTBOX_CLIENT_URL}/v1/status every {{ interval }}s"
    watch -n "{{ interval }}" "curl -s -H 'Authorization: Bearer ${SYFTBOX_CLIENT_TOKEN}' ${SYFTBOX_CLIENT_URL}/v1/status | jq .runtime"

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
    if [ -d "$SANDBOX_DIR" ]; then
        chmod -R u+w "$SANDBOX_DIR" 2>/dev/null || true
    fi
    rm -rf "$SANDBOX_DIR" || true
    PERF_TEST_SANDBOX="$SANDBOX_DIR" go test -v -timeout 30m -tags integration {{ ARGS }}
    echo "Test artifacts preserved at: $SANDBOX_DIR"

[group('devstack')]
sbdev-test-perf-rust *ARGS:
    #!/bin/bash
    set -eou pipefail
    echo "Running devstack performance tests with Rust client..."
    root_dir="$(pwd)"
    rust_bin="$root_dir/rust/target/release/syftbox-rs"
    cd rust && cargo build --release && cd "$root_dir"
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
    SBDEV_CLIENT_BIN="$rust_bin" PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -v -timeout 30m -tags integration {{ ARGS }}
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
    if [ -d "$SANDBOX_DIR" ]; then
        chmod -R u+w "$SANDBOX_DIR" 2>/dev/null || true
        # Retry removal a few times in case devstack processes are still exiting.
        for _ in 1 2 3; do
            rm -rf "$SANDBOX_DIR" 2>/dev/null && break
            sleep 0.5
        done
        rm -rf "$SANDBOX_DIR" || true
    fi
    PERF_TEST_SANDBOX="$SANDBOX_DIR" PERF_PROFILE=1 go test -v -timeout 30m -tags integration {{ ARGS }}
    echo ""
    echo "Profiles saved to: cmd/devstack/profiles/"
    echo "View flame graphs: go tool pprof -http=:8080 cmd/devstack/profiles/{test}/cpu.prof"

[group('devstack')]
sbdev-test-perf-profile-rust *ARGS:
    #!/bin/bash
    set -eou pipefail
    echo "Running performance tests with profiling enabled (Rust client)..."
    root_dir="$(pwd)"
    rust_bin="$root_dir/rust/target/release/syftbox-rs"
    cd rust && cargo build --release && cd "$root_dir"
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
    SBDEV_CLIENT_BIN="$rust_bin" PERF_TEST_SANDBOX="$SANDBOX_DIR" PERF_PROFILE=1 GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -v -timeout 30m -tags integration {{ ARGS }}
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
sbdev-test-perf-sandbox-rust *ARGS:
    #!/bin/bash
    set -eou pipefail
    root_dir="$(pwd)"
    REPO_ROOT="$root_dir"
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) sandbox_path="$PERF_TEST_SANDBOX" ;;
            *) sandbox_path="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        sandbox_path="$REPO_ROOT/.test-sandbox/perf-sandbox"
    fi
    echo "Running performance tests with persistent sandbox (Rust client): $sandbox_path"
    rm -rf "$sandbox_path"
    rust_bin="$root_dir/rust/target/release/syftbox-rs"
    cd rust && cargo build --release && cd "$root_dir"
    cd cmd/devstack
    SBDEV_CLIENT_BIN="$rust_bin" PERF_TEST_SANDBOX="$sandbox_path" go test -v -timeout 30m -tags integration {{ ARGS }}
    echo ""
    echo "Test files preserved in: $sandbox_path"
    echo "Files from alice: $sandbox_path/alice@example.com/datasites/alice@example.com/public/"
    echo "Files synced to bob: $sandbox_path/bob@example.com/datasites/alice@example.com/public/"

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

# Integration test groups - each group can run in parallel in CI
# IMPORTANT: When adding new tests, add them to the appropriate group below
# Run `just sbdev-test-verify` to check all tests are covered
_test_group_core := "TestDevstackIntegration|TestACKNACKMechanism|TestParseStartFlagsSkipClientDaemons"
_test_group_acl := "TestACLEnablesDownload|TestACLPropagationUpdates|TestACLRaceCondition|TestACLChangeDuringUpload"
_test_group_conflict := "TestSimultaneousWrite|TestDivergentEdits|TestThreeWayConflict|TestConflictDuringACLChange|TestNestedPathConflict|TestJournalWriteTiming|TestNonConflictUpdate|TestRapidSequentialEdits|TestJournalLossRecovery|TestDeleteDuringDownload|TestDeleteDuringTempRename|TestOverwriteDuringDownload"
_test_group_journal := "TestJournalGapSpuriousConflict|TestJournalGapHealing"
_test_group_perf := "TestLargeFileTransfer|TestWebSocketLatency|TestConcurrentUploads|TestManySmallFiles|TestFileModificationDuringSync|TestProfilePerformance"
_test_group_upload := "TestLargeUploadViaDaemon|TestLargeUploadViaDaemonStress|TestProgressAPI|TestProgressAPIWithUpload|TestProgressAPIDemo|TestProgressAPIPauseResumeUpload"
_test_group_ws := "TestWebSocketReconnectAfterServerRestart"
_test_group_chaos := "TestChaosSync"
_test_group_all := _test_group_core + "|" + _test_group_acl + "|" + _test_group_conflict + "|" + _test_group_journal + "|" + _test_group_perf + "|" + _test_group_upload + "|" + _test_group_ws + "|" + _test_group_chaos

[group('devstack')]
sbdev-test-group group mode="go":
    #!/bin/bash
    set -eou pipefail
    MODE_RAW="{{mode}}"
    MODE_RAW="${MODE_RAW#mode=}"
    MODE="$(echo "$MODE_RAW" | tr '[:upper:]' '[:lower:]')"
    GROUP="{{group}}"

    # Map group name to pattern
    case "$GROUP" in
        core)     PATTERN="{{_test_group_core}}" ;;
        acl)      PATTERN="{{_test_group_acl}}" ;;
        conflict) PATTERN="{{_test_group_conflict}}" ;;
        journal)  PATTERN="{{_test_group_journal}}" ;;
        perf)     PATTERN="{{_test_group_perf}}" ;;
        upload)   PATTERN="{{_test_group_upload}}" ;;
        ws)       PATTERN="{{_test_group_ws}}" ;;
        chaos)    PATTERN="{{_test_group_chaos}}" ;;
        all)      PATTERN="{{_test_group_all}}" ;;
        *)        echo "Unknown group: $GROUP"; echo "Valid groups: core, acl, conflict, journal, perf, upload, ws, chaos, all"; exit 1 ;;
    esac

    echo "Running integration test group '$GROUP' (mode=$MODE)..."
    root_dir="$(pwd)"
    rust_bin="$root_dir/rust/target/release/syftbox-rs"
    if [[ "$MODE" != "go" ]]; then
        cd rust && cargo build --release && cd "$root_dir"
        export SBDEV_RUST_CLIENT_BIN="$rust_bin"
        export SBDEV_CLIENT_MODE="$MODE"
    else
        unset SBDEV_CLIENT_MODE SBDEV_RUST_CLIENT_BIN
    fi

    SANDBOX_DIR="${PERF_TEST_SANDBOX:-$root_dir/.test-sandbox/$GROUP-tests}"
    echo "Sandbox: $SANDBOX_DIR"
    rm -rf "$SANDBOX_DIR"
    cd cmd/devstack
    PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 20m -tags integration -run "$PATTERN"
    echo ""
    echo "✅ Test group '$GROUP' completed (mode=$MODE)!"

[group('devstack')]
sbdev-test-all mode="go":
    #!/bin/bash
    set -eou pipefail
    MODE_RAW="{{mode}}"
    MODE_RAW="${MODE_RAW#mode=}"
    MODE="$(echo "$MODE_RAW" | tr '[:upper:]' '[:lower:]')"

    echo "Running devstack integration suite (mode=$MODE)..."
    root_dir="$(pwd)"
    rust_bin="$root_dir/rust/target/release/syftbox-rs"
    if [[ "$MODE" != "go" ]]; then
        cd rust && cargo build --release && cd "$root_dir"
        export SBDEV_RUST_CLIENT_BIN="$rust_bin"
        export SBDEV_CLIENT_MODE="$MODE"
    else
        unset SBDEV_CLIENT_MODE SBDEV_RUST_CLIENT_BIN
    fi

    SANDBOX_DIR="${PERF_TEST_SANDBOX:-$root_dir/.test-sandbox/all-tests}"
    echo "Sandbox: $SANDBOX_DIR"
    rm -rf "$SANDBOX_DIR"
    cd cmd/devstack
    PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 45m -tags integration -run "{{_test_group_all}}"
    echo ""
    echo "✅ Devstack integration suite completed (mode=$MODE)! Sandbox preserved at: $SANDBOX_DIR"

# Verify all integration tests are covered by a group
[group('devstack')]
sbdev-test-verify:
    #!/bin/bash
    set -eou pipefail
    echo "Verifying all integration tests are covered by groups..."
    cd cmd/devstack

    # Get all integration test names
    ALL_TESTS=$(go test -tags integration -list 'Test.*' . 2>/dev/null | grep '^Test' | sort)

    # Known test patterns from groups
    COVERED_PATTERN="{{_test_group_all}}"

    MISSING=""
    for test in $ALL_TESTS; do
        if ! echo "$test" | grep -qE "^($COVERED_PATTERN)$"; then
            MISSING="$MISSING $test"
        fi
    done

    if [[ -n "$MISSING" ]]; then
        echo "❌ The following tests are NOT covered by any group:"
        for test in $MISSING; do
            echo "  - $test"
        done
        echo ""
        echo "Add them to the appropriate _test_group_* variable in justfile"
        exit 1
    fi

    echo "✅ All $(echo "$ALL_TESTS" | wc -l | tr -d ' ') integration tests are covered!"
    echo ""
    echo "Groups:"
    echo "  core:     {{_test_group_core}}"
    echo "  acl:      {{_test_group_acl}}"
    echo "  conflict: {{_test_group_conflict}}"
    echo "  journal:  {{_test_group_journal}}"
    echo "  perf:     {{_test_group_perf}}"
    echo "  upload:   {{_test_group_upload}}"
    echo "  ws:       {{_test_group_ws}}"
    echo "  chaos:    {{_test_group_chaos}}"

[group('devstack')]
sbdev-test-all-serial mode="go":
    #!/bin/bash
    set -eou pipefail
    MODE_RAW="{{mode}}"
    MODE_RAW="${MODE_RAW#mode=}"
    MODE="$(echo "$MODE_RAW" | tr '[:upper:]' '[:lower:]')"

    echo "Running devstack integration suite one test at a time (mode=$MODE)..."
    root_dir="$(pwd)"
    rust_bin="$root_dir/rust/target/release/syftbox-rs"
    if [[ "$MODE" != "go" ]]; then
        cd rust && cargo build --release && cd "$root_dir"
        export SBDEV_RUST_CLIENT_BIN="$rust_bin"
        export SBDEV_CLIENT_MODE="$MODE"
    else
        unset SBDEV_CLIENT_MODE SBDEV_RUST_CLIENT_BIN
    fi

    BASE_SANDBOX="${PERF_TEST_SANDBOX:-$root_dir/.test-sandbox/all-tests-serial}"
    echo "Sandbox base: $BASE_SANDBOX"

    TESTS=(
        TestACKNACKMechanism
        TestDevstackIntegration
        TestACLEnablesDownload
        TestACLPropagationUpdates
        TestACLRaceCondition
        TestWebSocketReconnectAfterServerRestart
        TestSimultaneousWrite
        TestDivergentEdits
        TestThreeWayConflict
        TestConflictDuringACLChange
        TestNestedPathConflict
        TestJournalWriteTiming
        TestNonConflictUpdate
        TestRapidSequentialEdits
        TestJournalLossRecovery
        TestJournalGapSpuriousConflict
        TestJournalGapHealing
        TestFileModificationDuringSync
        TestWebSocketLatency
        TestProgressAPI
        TestProgressAPIWithUpload
        TestProgressAPIDemo
        TestLargeFileTransfer
        TestConcurrentUploads
        TestManySmallFiles
        TestLargeUploadViaDaemon
        TestLargeUploadViaDaemonStress
    )

    cd cmd/devstack
    for TEST in "${TESTS[@]}"; do
        SANDBOX_DIR="$BASE_SANDBOX/$TEST"
        echo "=== $TEST (sandbox: $SANDBOX_DIR) ==="
        rm -rf "$SANDBOX_DIR"
        PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 45m -tags integration -run "^${TEST}$"
        echo "Test artifacts preserved at: $SANDBOX_DIR"
    done

    echo ""
    echo "Devstack integration serial suite completed (mode=$MODE)! Sandbox preserved at: $BASE_SANDBOX"

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
sbdev-test-acl-rust:
    #!/bin/bash
    set -eou pipefail
    echo "Running ACL race condition test with Rust client..."
    root_dir="$(pwd)"
    rust_bin="$root_dir/rust/target/release/syftbox-rs"
    cd rust && cargo build --release && cd "$root_dir"
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        SANDBOX_DIR="$REPO_ROOT/.test-sandbox/acl-test"
    fi
    rm -rf "$SANDBOX_DIR"
    SBDEV_CLIENT_BIN="$rust_bin" PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 10m -tags integration -run TestACLRaceCondition
    echo "Test artifacts preserved at: $SANDBOX_DIR"

[group('devstack')]
sbdev-test-acl-prop-rust:
    #!/bin/bash
    set -eou pipefail
    RUNS=${1:-1}
    shift || true
    echo "Running ACL propagation regression test with Rust client ($RUNS time(s))..."
    root_dir="$(pwd)"
    rust_bin="$root_dir/rust/target/release/syftbox-rs"
    cd rust && cargo build --release && cd "$root_dir"
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
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
        SBDEV_CLIENT_BIN="$rust_bin" PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 10m -tags integration -run TestACLPropagationUpdates "$@"
        echo "Test artifacts preserved at: $SANDBOX_DIR"
    done

[group('devstack')]
sbdev-test-cleanup-rust:
    #!/bin/bash
    set -euo pipefail
    echo "Cleaning up test sandbox..."
    cd cmd/devstack
    go run . stop --path ../../sandbox 2>/dev/null || echo "Test sandbox not running"

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
sbdev-test-ws-rust:
    #!/bin/bash
    set -eou pipefail
    echo "Running WebSocket latency test with Rust client..."
    root_dir="$(pwd)"
    rust_bin="$root_dir/rust/target/release/syftbox-rs"
    cd rust && cargo build --release && cd "$root_dir"
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
    SBDEV_CLIENT_BIN="$rust_bin" PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 10m -tags integration -run TestWebSocketLatency
    echo "Test artifacts preserved at: $SANDBOX_DIR"

[group('devstack')]
sbdev-test-large-sync:
    #!/bin/bash
    set -eou pipefail
    echo "Running large file sync test (daemon end-to-end)..."
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
sbdev-test-large:
    just sbdev-test-large-sync

[group('devstack')]
sbdev-test-large-upload-daemon:
    #!/bin/bash
    set -eou pipefail
    echo "Running large upload via daemon test..."
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
    if [ -d "$SANDBOX_DIR" ]; then
        chmod -R u+w "$SANDBOX_DIR" 2>/dev/null || true
    fi
    rm -rf "$SANDBOX_DIR" || true
    PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -v -timeout 60m -tags integration -run TestLargeUploadViaDaemon

[group('devstack')]
sbdev-test-large-upload:
    just sbdev-test-large-upload-daemon

[group('devstack')]
sbdev-test-large-upload-daemon-stress:
    #!/bin/bash
    set -eou pipefail
    echo "Running large upload via daemon stress test (kill/restart/resume)..."
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        SANDBOX_DIR="$REPO_ROOT/.test-sandbox/large-upload-stress"
    fi
    if [ -d "$SANDBOX_DIR" ]; then
        chmod -R u+w "$SANDBOX_DIR" 2>/dev/null || true
        for _ in 1 2 3; do
            rm -rf "$SANDBOX_DIR" 2>/dev/null && break
            sleep 0.5
        done
        rm -rf "$SANDBOX_DIR" || true
    fi
    PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -v -timeout 60m -tags integration -run TestLargeUploadViaDaemonStress

[group('devstack')]
sbdev-test-large-upload-stress:
    just sbdev-test-large-upload-daemon-stress

[group('devstack')]
sbdev-test-large-upload-rust:
    #!/bin/bash
    set -eou pipefail
    echo "Running large upload via daemon test with Rust client..."
    root_dir="$(pwd)"
    rust_bin="$root_dir/rust/target/release/syftbox-rs"
    cd rust && cargo build --release && cd "$root_dir"
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
    SBDEV_CLIENT_BIN="$rust_bin" PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -v -timeout 60m -tags integration -run TestLargeUploadViaDaemon

[group('devstack')]
sbdev-test-large-upload-daemon-stress-rust:
    #!/bin/bash
    set -eou pipefail
    echo "Running large upload via daemon stress test with Rust client (kill/restart/resume)..."
    root_dir="$(pwd)"
    rust_bin="$root_dir/rust/target/release/syftbox-rs"
    cd rust && cargo build --release && cd "$root_dir"
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        SANDBOX_DIR="$REPO_ROOT/.test-sandbox/large-upload-stress"
    fi
    if [ -d "$SANDBOX_DIR" ]; then
        chmod -R u+w "$SANDBOX_DIR" 2>/dev/null || true
        for _ in 1 2 3; do
            rm -rf "$SANDBOX_DIR" 2>/dev/null && break
            sleep 0.5
        done
        rm -rf "$SANDBOX_DIR" || true
    fi
    SBDEV_CLIENT_BIN="$rust_bin" PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -v -timeout 60m -tags integration -run TestLargeUploadViaDaemonStress

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
sbdev-test-progress-api-rust:
    #!/bin/bash
    set -eou pipefail
    echo "Running Progress API demo with Rust client..."
    echo "This demo shows the sync status and upload management APIs in action."
    echo "Features: status tracking, progress bars, pause/resume, error handling, auth"
    echo ""
    root_dir="$(pwd)"
    rust_bin="$root_dir/rust/target/release/syftbox-rs"
    cd rust && cargo build --release && cd "$root_dir"
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
    SBDEV_CLIENT_BIN="$rust_bin" PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -v -timeout 15m -tags integration -run TestProgressAPIDemo

[group('devstack')]
sbdev-watch-test recipe interval="0.5" env_file="":
    #!/bin/bash
    set -euo pipefail

    REPO_ROOT="$(pwd)"
    if [ -z "{{ env_file }}" ]; then
        ENV_FILE="$REPO_ROOT/.test-sandbox/watch.env"
    else
        ENV_FILE="{{ env_file }}"
    fi
    rm -f "$ENV_FILE"

    echo "Starting test recipe: {{ recipe }}"
    SBDEV_WATCH_ENV="$ENV_FILE" just "{{ recipe }}" &
    TEST_PID=$!

    # Wait for env file to appear (max ~30s)
    for i in $(seq 1 120); do
        if [ -f "$ENV_FILE" ] && grep -q "SYFTBOX_CLIENT_URL=" "$ENV_FILE" && grep -q "SYFTBOX_CLIENT_TOKEN=" "$ENV_FILE"; then
            break
        fi
        sleep 0.25
    done

    if [ ! -f "$ENV_FILE" ]; then
        echo "Env file was not created by test; tailing test output only." >&2
        wait "$TEST_PID"
        exit 1
    fi

    echo "Env file ready at $ENV_FILE"
    # shellcheck disable=SC1090
    source "$ENV_FILE"
    echo "Watching runtime status every {{ interval }}s until test exits (Ctrl-C to stop early)..."

    cleanup() {
        echo ""
        echo "Stopping test (pid $TEST_PID)"
        kill "$TEST_PID" 2>/dev/null || true
        wait "$TEST_PID" 2>/dev/null || true
        exit 0
    }
    trap cleanup INT TERM

    while kill -0 "$TEST_PID" 2>/dev/null; do
        curl -s -H "Authorization: Bearer ${SYFTBOX_CLIENT_TOKEN}" "${SYFTBOX_CLIENT_URL}/v1/status" | jq .runtime
        sleep "{{ interval }}"
    done

    echo "Test finished; stopping watch."
    wait "$TEST_PID" 2>/dev/null || true

[group('devstack')]
sbdev-test-large-rust:
    #!/bin/bash
    set -eou pipefail
    echo "Running large file transfer test with Rust client..."
    root_dir="$(pwd)"
    rust_bin="$root_dir/rust/target/release/syftbox-rs"
    cd rust && cargo build --release && cd "$root_dir"
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
    SBDEV_CLIENT_BIN="$rust_bin" PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -v -timeout 30m -tags integration -run TestLargeFileTransfer

[group('devstack')]
sbdev-test-concurrent mode="go":
    #!/bin/bash
    set -eou pipefail
    MODE_RAW="{{mode}}"
    MODE_RAW="${MODE_RAW#mode=}"
    MODE="$(echo "$MODE_RAW" | tr '[:upper:]' '[:lower:]')"

    echo "Running concurrent upload test (mode=$MODE)..."
    root_dir="$(pwd)"
    rust_bin="$root_dir/rust/target/release/syftbox-rs"
    if [[ "$MODE" != "go" ]]; then
        cd rust && cargo build --release && cd "$root_dir"
        export SBDEV_RUST_CLIENT_BIN="$rust_bin"
        export SBDEV_CLIENT_MODE="$MODE"
    else
        unset SBDEV_CLIENT_MODE SBDEV_RUST_CLIENT_BIN
    fi

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

    # Run all conflict tests in one go test process; harness resets state per test.
    RUN_REGEX="TestSimultaneousWrite|TestDivergentEdits|TestThreeWayConflict|TestConflictDuringACLChange|TestNestedPathConflict|TestJournalWriteTiming|TestNonConflictUpdate|TestRapidSequentialEdits|TestJournalLossRecovery"
    for i in $(seq 1 "$RUNS"); do
        if [ "$RUNS" -gt 1 ]; then
            SANDBOX_DIR="${BASE_SANDBOX}-${i}"
            echo "Run $i/$RUNS using sandbox: $SANDBOX_DIR"
        else
            SANDBOX_DIR="$BASE_SANDBOX"
            echo "Using sandbox: $SANDBOX_DIR"
        fi

        rm -rf "$SANDBOX_DIR"
        PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 10m -tags integration -run "^(${RUN_REGEX})$" "$@" || exit 1
        echo "Test artifacts preserved at: $SANDBOX_DIR"
    done

[group('devstack')]
sbdev-test-conflict-rust:
    #!/bin/bash
    set -eou pipefail
    echo "Running conflict resolution tests with Rust client..."
    RUNS=${1:-1}
    shift || true
    root_dir="$(pwd)"
    rust_bin="$root_dir/rust/target/release/syftbox-rs"
    cd rust && cargo build --release && cd "$root_dir"
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

    TESTS=("TestSimultaneousWrite" "TestDivergentEdits" "TestThreeWayConflict" "TestConflictDuringACLChange" "TestNestedPathConflict")
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
            SBDEV_CLIENT_BIN="$rust_bin" PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 10m -tags integration -run "^${TEST}$" "$@" || exit 1
        done
        echo "Test artifacts preserved at: $SANDBOX_DIR"
    done

[group('devstack')]
sbdev-test-concurrent-rust:
    #!/bin/bash
    # Back-compat shim: call unified recipe with rust mode
    just sbdev-test-concurrent mode="rust"

[group('devstack')]
sbdev-test-nested-loop RUNS='10' *ARGS:
    #!/bin/bash
    set -eou pipefail
    RUNS_RAW="{{RUNS}}"
    if [[ "$RUNS_RAW" == RUNS=* ]]; then
        RUNS_RAW="${RUNS_RAW#RUNS=}"
    fi
    if ! [[ "$RUNS_RAW" =~ ^[0-9]+$ ]] || [ "$RUNS_RAW" -lt 1 ]; then
        echo "Usage: just sbdev-test-nested-loop [RUNS] [go test args...]"
        echo "RUNS must be a positive integer (got: '$RUNS_RAW')"
        exit 2
    fi
    RUNS="$RUNS_RAW"
    echo "Running all integration tests up to $RUNS times (stop on first failure)..."
    cd cmd/devstack
    REPO_ROOT="$(pwd)/../.."
    if [ -n "${PERF_TEST_SANDBOX:-}" ]; then
        case "$PERF_TEST_SANDBOX" in
            /*) SANDBOX_DIR="$PERF_TEST_SANDBOX" ;;
            *) SANDBOX_DIR="$REPO_ROOT/$PERF_TEST_SANDBOX" ;;
        esac
    else
        SANDBOX_DIR="$REPO_ROOT/.test-sandbox/all-tests-loop"
    fi
    # Default test set for this loop (avoids the longest/most expensive tests).
    RUN_REGEX="TestACLRaceCondition|TestWebSocketLatency|TestLargeFileTransfer|TestConcurrentUploads|TestSimultaneousWrite|TestDivergentEdits|TestThreeWayConflict|TestConflictDuringACLChange|TestNestedPathConflict|TestJournalWriteTiming|TestNonConflictUpdate|TestRapidSequentialEdits|TestJournalLossRecovery|TestManySmallFiles|TestACKNACKMechanism"

    # Parse optional go test args. (If the caller uses `just ... -- <args>`, strip the leading `--`.)
    ARGS=( {{ ARGS }} )
    if [ "${#ARGS[@]}" -gt 0 ] && [ "${ARGS[0]}" = "--" ]; then
        ARGS=("${ARGS[@]:1}")
    fi

    # Kill any orphaned processes from previous runs using this sandbox
    if pgrep -f "$SANDBOX_DIR" > /dev/null 2>&1; then
        echo "Killing orphaned processes from previous runs..."
        pkill -f "$SANDBOX_DIR" 2>/dev/null || true
        sleep 1
    fi
    for ((i = 1; i <= RUNS; i++)); do
        echo "=== Run $i/$RUNS (sandbox: $SANDBOX_DIR) ==="
        pkill -f "$SANDBOX_DIR" 2>/dev/null || true
        sleep 0.5
        rm -rf "$SANDBOX_DIR"
        PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 15m -tags integration -run "^(${RUN_REGEX})$" "${ARGS[@]}" || {
            echo "FAILED on run $i (sandbox preserved at $SANDBOX_DIR)"
            exit 1
        }
    done
    echo "PASSED all $RUNS runs"

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

    # Run all race tests in one go test process; harness resets state per test.
    RUN_REGEX="TestDeleteDuringDownload|TestACLChangeDuringUpload|TestOverwriteDuringDownload|TestDeleteDuringTempRename"
    for i in $(seq 1 "$RUNS"); do
        if [ "$RUNS" -gt 1 ]; then
            SANDBOX_DIR="${BASE_SANDBOX}-${i}"
            echo "Run $i/$RUNS using sandbox: $SANDBOX_DIR"
        else
            SANDBOX_DIR="$BASE_SANDBOX"
            echo "Using sandbox: $SANDBOX_DIR"
        fi

        rm -rf "$SANDBOX_DIR"
        PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 10m -tags integration -run "^(${RUN_REGEX})$" "$@" || exit 1
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

[group('devstack')]
sbdev-test-many-rust:
    #!/bin/bash
    set -eou pipefail
    echo "Running many small files test with Rust client..."
    root_dir="$(pwd)"
    rust_bin="$root_dir/rust/target/release/syftbox-rs"
    cd rust && cargo build --release && cd "$root_dir"
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
    SBDEV_CLIENT_BIN="$rust_bin" PERF_TEST_SANDBOX="$SANDBOX_DIR" GOCACHE="${GOCACHE:-$(pwd)/.gocache}" go test -count=1 -v -timeout 15m -tags integration -run TestManySmallFiles
    echo "Test artifacts preserved at: $SANDBOX_DIR"

[group('devstack')]
sbdev-test-rust *ARGS:
    #!/bin/bash
    set -euo pipefail
    root_dir="$(pwd)"
    rust_client_path="$root_dir/rust/target/release/syftbox-rs"

    echo "Building Rust client at $rust_client_path..."
    cd rust
    cargo build --release
    cd "$root_dir"

    echo "Running devstack tests with Rust client..."
    SBDEV_CLIENT_BIN="$rust_client_path" \
    GOCACHE="$root_dir/.gocache" \
    go test -v -timeout 30m -tags integration ./cmd/devstack {{ ARGS }}

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
