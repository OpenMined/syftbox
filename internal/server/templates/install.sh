#!/bin/sh
# Licensed under the Apache License, Version 2.0

set -e

# --no-prompt => disables the run client prompt
ASK_RUN_CLIENT=1

# --run => disables the prompt & runs the client
RUN_CLIENT=0

APP_NAME="syftbox"
ARTIFACT_BASE_URL=${ARTIFACT_BASE_URL:-"https://syftboxdev.openmined.org"}
ARTIFACT_DOWNLOAD_URL="$ARTIFACT_BASE_URL/releases"

red='\033[1;31m'
yellow='\033[0;33m'
cyan='\033[0;36m'
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
    if check_cmd curl
    then curl -fs "$1" -o "$2"
    elif check_cmd wget
    then wget -q "$1" -O "$2" 
    else need_cmd "curl or wget"
    fi
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

###################################################

prompt_restart_shell() {
    echo
    warn "RESTART your shell or RELOAD shell profile"
    echo "  \`source ~/.zshrc\`        (for zsh)"
    echo "  \`source ~/.bash_profile\` (for bash)"
    echo "  \`source ~/.profile\`      (for sh)"

    success "\nAfter reloading, start the client"
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
    exec ~/.local/bin/syftbox < /dev/tty
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
        rm -f "$HOME/.local/bin/syftbox"
    fi
}

pre_install() {
    need_cmd "uname"
    need_cmd "tar"
    need_cmd "mktemp"
    need_cmd "rm"

    uninstall_old_version
}

post_install() {
    success "Installation completed! $(~/.local/bin/syftbox -v)"

    if [ $RUN_CLIENT -eq 1 ]
    then run_client
    elif [ $ASK_RUN_CLIENT -eq 1 ]
    then prompt_run_client
    else prompt_restart_shell
    fi
}

install_syftbox() {
    local os=$(detect_os)
    local arch=$(detect_arch)
    local pkg_name="${APP_NAME}_client_${os}_${arch}"
    local tmp_dir=$(mktemp -d)

    info "Downloading SyftBox"
    mkdir -p $tmp_dir
    downloader "${ARTIFACT_DOWNLOAD_URL}/${pkg_name}.tar.gz" "$tmp_dir/$pkg_name.tar.gz"

    info "Installing SyftBox"
    tar -xzf "$tmp_dir/$pkg_name.tar.gz" -C $tmp_dir
    mkdir -p $HOME/.local/bin
    cp "$tmp_dir/$pkg_name/syftbox" $HOME/.local/bin/syftbox

    rm -rf $tmp_dir
    patch_path
}

do_install() {
    for arg in "$@"; do
        case "$arg" in
            -r|--run|run)
                RUN_CLIENT=1
                ;;
            -n|--no-prompt|no-prompt)
                ASK_RUN_CLIENT=0
                ;;
            *)
                ;;
        esac
    done

    pre_install
    install_syftbox
    post_install
}

do_install "$@" || exit 1
