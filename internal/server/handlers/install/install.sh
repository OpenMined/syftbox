#!/bin/sh
# Licensed under the Apache License, Version 2.0

set -e

# Installation modes:
# - download-only: Just download the binary, no login/run, no prompts
# - setup-only: Download + login, but don't run, no prompts  
# - interactive: Download + login + prompt to run (default)

# Default installation mode (can be overridden by INSTALL_MODE env var)
INSTALL_MODE=${INSTALL_MODE:-"interactive"}

# Apps to install (comma separated list)
INSTALL_APPS=${INSTALL_APPS:-""}

# Debug mode (can be overridden by SYFTBOX_DEBUG env var)
DEBUG=${DEBUG:-"0"}

APP_NAME="syftbox"
ARTIFACT_BASE_URL=${ARTIFACT_BASE_URL:-"https://syftbox.net"}
ARTIFACT_DOWNLOAD_URL="$ARTIFACT_BASE_URL/releases"
SYFTBOX_BINARY_PATH="$HOME/.local/bin/syftbox"

red='\033[1;31m'
yellow='\033[0;33m'
cyan='\033[0;36m'
purple='\033[0;35m'
green='\033[1;32m'
reset='\033[0m'

err() {
    echo "${red}ERROR${reset}: $1" >&2
    exit 1
}

info() {
    echo "${cyan}$1${reset}"
}

warn() {
    echo "${yellow}$1${reset}"
}

success() {
    echo "${green}$1${reset}"
}

debug() {
    if [ "$DEBUG" = "1" ]; then
        echo "${purple}DEBUG${reset}: $1" >&2
    fi
}

check_cmd() {
    command -v "$1" > /dev/null 2>&1
    return $?
}

need_cmd() {
    if ! check_cmd "$1"
    then err "need '$1' (command not found)"
    fi
}

###################################################

downloader() {
    local url="$1"
    local output="$2"

    debug "attempting download: $url -> $output"

    if check_cmd curl; then
        local curl_flags="-fSL --progress-bar"
        if [ "$DEBUG" = "1" ]; then
            curl_flags="-v $curl_flags"
        fi
        curl $curl_flags "$url" -o "$output"
        local exit_code=$?
        if [ $exit_code -ne 0 ]; then
            err "failed to download $url (curl exit code: $exit_code)"
        fi
    elif check_cmd wget; then
        local wget_flags="-q --show-progress --https-only"
        if [ "$DEBUG" = "1" ]; then
            wget_flags="-d $wget_flags"
        fi
        wget $wget_flags "$url" -O "$output"
        local exit_code=$?
        if [ $exit_code -ne 0 ]; then
            err "failed to download $url (wget exit code: $exit_code)"
        fi
    else
        need_cmd "curl or wget"
    fi

    debug "download completed successfully"
}

###################################################

check_home_path() {
    # check if a path exists as ~/path or $HOME/path
    if echo $PATH | grep -q "$HOME/$1" || echo $PATH | grep -q "~/$1"
    then return 0
    else return 1
    fi
}

write_path() {
    local _path_contents="$1"
    local _profile_path="$2"
    # if profile exists, add the export
    if [ -f "$_profile_path" ]
    then
        echo "export PATH=\"$_path_contents\$PATH\"" >> $_profile_path;
    fi
}

patch_path() {
    local _path_expr=""

    if ! check_home_path ".local/bin"
    then _path_expr="${_path_expr}$HOME/.local/bin:"
    fi

    # reload env vars
    export PATH="$_path_expr$PATH"

    # write to profile files
    write_path $_path_expr "$HOME/.profile"
    write_path $_path_expr "$HOME/.zshrc"
    write_path $_path_expr "$HOME/.bashrc"
    write_path $_path_expr "$HOME/.bash_profile"
}

###################################################

# Detect OS type
detect_os() {
  case "$(uname -s)" in
    Darwin*)
      echo "darwin"
      ;;
    Linux*)
      echo "linux"
      ;;
    *)
      error "Unsupported operating system: $(uname -s)"
      exit 1
      ;;
  esac
}

# Detect architecture
detect_arch() {
  local arch
  arch=$(uname -m)
  
  case "$arch" in
    x86_64|amd64)
      echo "amd64"
      ;;
    arm64|aarch64)
      echo "arm64"
      ;;
    *)
      error "Unsupported architecture: $arch"
      exit 1
      ;;
  esac
}

debug_dump() {
    if [ "$DEBUG" = "1" ]; then
        echo "${yellow}=== SYFTBOX INSTALLER DEBUG ===${reset}" >&2
        echo "${yellow}System Information:${reset}" >&2
        echo "  OS: $(uname -s)" >&2
        echo "  Architecture: $(uname -m)" >&2
        echo "  Detected OS: $(detect_os)" >&2
        echo "  Detected Arch: $(detect_arch)" >&2
        echo "  Shell: $SHELL" >&2
        echo "  User: $USER" >&2
        echo "  Home: $HOME" >&2
        echo >&2
        echo "${yellow}Configuration:${reset}" >&2
        echo "  Install Mode: $INSTALL_MODE" >&2
        echo "  Install Apps: $INSTALL_APPS" >&2
        echo "  Artifact Base URL: $ARTIFACT_BASE_URL" >&2
        echo "  Download URL: $ARTIFACT_DOWNLOAD_URL" >&2
        echo "  Binary Path: $SYFTBOX_BINARY_PATH" >&2
        echo "  Package Name: ${APP_NAME}_client_$(detect_os)_$(detect_arch)" >&2
        echo >&2
        echo "${yellow}Environment Variables:${reset}" >&2
        echo "  SYFTBOX_INSTALL_MODE: ${SYFTBOX_INSTALL_MODE:-"(not set)"}" >&2
        echo "  SYFTBOX_INSTALL_APPS: ${SYFTBOX_INSTALL_APPS:-"(not set)"}" >&2
        echo "  SYFTBOX_ARTIFACT_BASE_URL: ${SYFTBOX_ARTIFACT_BASE_URL:-"(not set)"}" >&2
        echo "  SYFTBOX_DEBUG: ${SYFTBOX_DEBUG:-"(not set)"}" >&2
        echo >&2
        echo "${yellow}PATH Information:${reset}" >&2
        echo "  Current PATH: $PATH" >&2
        echo "  ~/.local/bin in PATH: $(check_home_path ".local/bin" && echo "yes" || echo "no")" >&2
        echo >&2
        echo "${yellow}Dependencies:${reset}" >&2
        echo "  curl: $(check_cmd curl && echo "available" || echo "not found")" >&2
        echo "  wget: $(check_cmd wget && echo "available" || echo "not found")" >&2
        echo "  tar: $(check_cmd tar && echo "available" || echo "not found")" >&2
        echo "  uname: $(check_cmd uname && echo "available" || echo "not found")" >&2
        echo "  mktemp: $(check_cmd mktemp && echo "available" || echo "not found")" >&2
        echo >&2
    fi
}

###################################################

prompt_restart_shell() {
    echo
    warn "RESTART your shell or RELOAD shell profile"
    echo "  \`source ~/.zshrc\`        (for zsh)"
    echo "  \`source ~/.bash_profile\` (for bash)"
    echo "  \`source ~/.profile\`      (for sh)"

    success "\nAfter reloading, login and start the client"
    echo "  \`syftbox login\`"
    echo "  \`syftbox\`"
}

###################################################
# Download & Install SyftBox
# 
# Packages
# syftbox_client_darwin_arm64.tar.gz
# syftbox_client_darwin_amd64.tar.gz
# syftbox_client_linux_arm64.tar.gz
# syftbox_client_linux_amd64.tar.gz

run_client() {
    echo
    success "Starting SyftBox client..."
    exec $SYFTBOX_BINARY_PATH
}

setup_client() {
    info "Setting up..."
    # Run login command and capture exit code
    if ! $SYFTBOX_BINARY_PATH login --quiet; then
        return 1
    fi

    if [ -n "$INSTALL_APPS" ];
    then
        info "Installing SyftBox Apps..."
        original_ifs="$IFS"
        IFS=','
        set -f
        for app in $INSTALL_APPS
        do
            echo "* $app"
            $SYFTBOX_BINARY_PATH app install $app || true
        done
        set +f
        IFS="$original_ifs"
    fi

    return 0
}

prompt_run_client() {
    # prompt if they want to start the client
    echo
    prompt=$(echo "${yellow}Start the client now? [y/n] ${reset}")
    while [ "$start_client" != "y" ] && [ "$start_client" != "Y" ] && [ "$start_client" != "n" ] && [ "$start_client" != "N" ]
    do
        read -p "$prompt" start_client < /dev/tty
    done

    if [ "$start_client" = "y" ] || [ "$start_client" = "Y" ]
    then run_client
    else prompt_restart_shell
    fi
}

uninstall_old_version() {
    if check_cmd syftbox
    then
        local path=$(command -v syftbox)
        info "Found old version of SyftBox ($path). Removing..."

        if check_cmd uv && uv tool list 2>/dev/null | grep -q syftbox
        then uv tool uninstall -q syftbox
        elif check_cmd pip && pip list 2>/dev/null | grep -q syftbox
        then pip uninstall -y syftbox
        fi

        # just yank the path to confirm
        rm -f "$path"
        rm -f "$SYFTBOX_BINARY_PATH"
    fi
}

pre_install() {
    debug "Starting pre-install checks..."
    need_cmd "uname"
    need_cmd "tar"
    need_cmd "mktemp"
    need_cmd "rm"

    uninstall_old_version
}

post_install() {
    case "$INSTALL_MODE" in
        "download-only")
            debug "install mode: download-only. skipping login and run"
            success "Download completed!"
            prompt_restart_shell
            ;;
        "setup-only")
            debug "install mode: setup-only. performing login but not running"
            if ! setup_client; then
                prompt_restart_shell
                echo
                err "Setup did not complete. Please login manually."
            fi
            success "Installation and setup completed!"
            prompt_restart_shell
            ;;
        "interactive")
            debug "install mode: interactive. performing login and prompting for run"
            if ! setup_client; then
                prompt_restart_shell
                echo
                err "Setup did not complete. Please login manually."
            fi
            success "Installation completed!"
            prompt_run_client
            ;;
        *)
            err "invalid install mode: $INSTALL_MODE"
            ;;
    esac
}

install_syftbox() {
    local os=$(detect_os)
    local arch=$(detect_arch)
    local pkg_name="${APP_NAME}_client_${os}_${arch}"
    local tmp_dir=$(mktemp -d)

    debug "Detected OS: $os"
    debug "Detected architecture: $arch"
    debug "Package name: $pkg_name"
    debug "Temporary directory: $tmp_dir"
    debug "Download URL: ${ARTIFACT_DOWNLOAD_URL}/${pkg_name}.tar.gz"

    info "Downloading..."
    mkdir -p $tmp_dir
    downloader "${ARTIFACT_DOWNLOAD_URL}/${pkg_name}.tar.gz" "$tmp_dir/$pkg_name.tar.gz"

    debug "Download completed, extracting..."
    info "Installing..."
    tar -xzf "$tmp_dir/$pkg_name.tar.gz" -C $tmp_dir
    mkdir -p $HOME/.local/bin
    cp "$tmp_dir/$pkg_name/syftbox" $SYFTBOX_BINARY_PATH
    debug "Binary copied to: $SYFTBOX_BINARY_PATH"
    info "Installed $($SYFTBOX_BINARY_PATH -v)"

    debug "Cleaning up temporary directory: $tmp_dir"
    rm -rf $tmp_dir
    debug "Patching PATH..."
    patch_path
}

show_usage() {
    echo "Usage: $0 [OPTIONS]"
    echo
    echo "Installation modes:"
    echo "  --download-only     Download binary only, no login/run"
    echo "  --setup-only        Download + login, but don't run"
    echo "  --interactive       Download + login + prompt to run (default)"
    echo
    echo "Options:"
    echo "  -m, --mode=MODE     Set installation mode (download-only|setup-only|interactive)"
    echo "  -a, --apps=APPS     Install comma-separated list of apps"
    echo "  -d, --debug         Enable debug output"
    echo "  -h, --help          Show this help message"
    echo
    echo "Environment variables:"
    echo "  SYFTBOX_INSTALL_MODE        Installation mode (default: interactive)"
    echo "  SYFTBOX_INSTALL_APPS        Apps to install (comma-separated)"
    echo "  SYFTBOX_ARTIFACT_BASE_URL   Base URL for downloads (default: https://syftbox.net)"
    echo "  SYFTBOX_DEBUG               Enable debug output (1=enabled, 0=disabled)"
}

do_install() {
    local next_arg=""
    for arg in "$@"; do
        case "$arg" in
            -h|--help)
                show_usage
                exit 0
                ;;
            --download-only)
                INSTALL_MODE="download-only"
                ;;
            --setup-only)
                INSTALL_MODE="setup-only"
                ;;
            --interactive)
                INSTALL_MODE="interactive"
                ;;
            -m=*|--mode=*)
                INSTALL_MODE="${arg#*=}"
                ;;
            -m|--mode)
                next_arg="mode"
                ;;
            -a=*|--apps=*)
                INSTALL_APPS="${arg#*=}"
                ;;
            -a|--apps)
                next_arg="apps"
                ;;
            -d|--debug)
                DEBUG="1"
                ;;
            *)
                if [ "$next_arg" = "mode" ]; then
                    INSTALL_MODE="$arg"
                    next_arg=""
                elif [ "$next_arg" = "apps" ]; then
                    INSTALL_APPS="$arg"
                    next_arg=""
                fi
                ;;
        esac
    done

    # Validate install mode
    case "$INSTALL_MODE" in
        "download-only"|"setup-only"|"interactive")
            ;;
        *)
            err "Invalid installation mode: $INSTALL_MODE. Use --help for usage."
            ;;
    esac

    debug_dump

    pre_install
    install_syftbox
    post_install
}

do_install "$@" || exit 1
