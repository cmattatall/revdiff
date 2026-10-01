#!/usr/bin/env bash
# Build and install revdiff and the Amp plugin. Compatible with macOS bash 3.2.
set -euo pipefail

usage() {
    echo "Usage: $0 [--no-path] [--shell bash|zsh]"
    echo "  --shell overrides automatic shell detection from \$SHELL"
}

UPDATE_PATH=1
TARGET_SHELL=${SHELL:-}
TARGET_SHELL=${TARGET_SHELL##*/}
while [ "$#" -gt 0 ]; do
    case "$1" in
        --no-path) UPDATE_PATH=0 ;;
        --shell)
            if [ "$#" -lt 2 ]; then usage >&2; exit 2; fi
            case "$2" in
                bash|zsh) TARGET_SHELL=$2 ;;
                *) echo "Unsupported shell: $2 (use bash or zsh)" >&2; exit 2 ;;
            esac
            shift
            ;;
        --help|-h) usage; exit 0 ;;
        *) usage >&2; exit 2 ;;
    esac
    shift
done

SOURCE_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SOURCE_DIR/../.." && pwd)
DEST_DIR="${HOME:?HOME must be set}/.config/amp/plugins"
DEST="$DEST_DIR/revdiff.ts"
BIN_DIR="$HOME/.local/bin"
BINARY="$BIN_DIR/revdiff"

check_destination() {
    if [ -L "$1" ] || { [ -e "$1" ] && [ ! -f "$1" ]; }; then
        echo "Refusing to replace a symlink or non-regular file: $1" >&2
        exit 1
    fi
}

check_destination "$DEST"
check_destination "$BINARY"
if ! command -v go >/dev/null 2>&1; then
    echo "Go is required to build revdiff from this checkout. Install Go, then rerun this script." >&2
    exit 1
fi

mkdir -p "$DEST_DIR" "$BIN_DIR"
PLUGIN_TEMP=""
BINARY_TEMP=""
trap 'rm -f "$PLUGIN_TEMP" "$BINARY_TEMP"' EXIT
BINARY_TEMP=$(mktemp "$BIN_DIR/.revdiff-install.XXXXXX")
printf 'Building revdiff from %s\n' "$REPO_ROOT"
(cd "$REPO_ROOT" && go build -trimpath -o "$BINARY_TEMP" ./app/revdiff)
PLUGIN_TEMP=$(mktemp "$DEST_DIR/.revdiff-install.XXXXXX")
cp "$SOURCE_DIR/revdiff.ts" "$PLUGIN_TEMP"
chmod 644 "$PLUGIN_TEMP"
chmod 755 "$BINARY_TEMP"
# Rename a fresh inode rather than overwriting a running macOS executable.
mv -f "$BINARY_TEMP" "$BINARY"
mv -f "$PLUGIN_TEMP" "$DEST"
printf 'Installed %s\nInstalled %s\n' "$BINARY" "$DEST"

add_path() {
    local rc=$1
    # Append through ordinary dotfile symlinks, preserving their target and mode.
    if { [ -e "$rc" ] || [ -L "$rc" ]; } && [ ! -f "$rc" ]; then
        printf 'Cannot update %s: not a regular startup file. Configure PATH manually.\n' "$rc" >&2
        return
    fi
    if [ -f "$rc" ] && grep -Fqx '# revdiff: user-local binaries' "$rc"; then
        printf 'PATH already configured in %s\n' "$rc"
        return
    fi
    if ! mkdir -p "$(dirname -- "$rc")" || ! cat >> "$rc" <<'EOF'

# revdiff: user-local binaries
case ":$PATH:" in
    *":$HOME/.local/bin:"*) ;;
    *) export PATH="$HOME/.local/bin:$PATH" ;;
esac
EOF
    then
        printf 'Could not update %s. Configure PATH manually.\n' "$rc" >&2
        return
    fi
    printf 'Added PATH setup to %s\n' "$rc"
}

if [ "$UPDATE_PATH" -eq 1 ]; then
    case "$TARGET_SHELL" in
        bash)
            add_path "$HOME/.bashrc"
            # Bash reads only the first existing login profile in this order.
            LOGIN_RC="$HOME/.bash_profile"
            for candidate in "$HOME/.bash_profile" "$HOME/.bash_login" "$HOME/.profile"; do
                if [ -e "$candidate" ] || [ -L "$candidate" ]; then
                    LOGIN_RC=$candidate
                    break
                fi
            done
            add_path "$LOGIN_RC"
            ;;
        zsh) add_path "${ZDOTDIR:-$HOME}/.zshrc" ;;
        *) printf 'Shell %s is not supported for automatic PATH setup; configure PATH manually.\n' "${TARGET_SHELL:-unknown}" >&2 ;;
    esac
fi
printf '\nTo use revdiff now in bash or zsh, run:\n  export PATH="$HOME/.local/bin:$PATH"\n'
printf 'New shell sessions will use the saved PATH setup, if configured.\n'
printf 'Reload Amp plugins or restart Amp to activate the plugin.\n'
