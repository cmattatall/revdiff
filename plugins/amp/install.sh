#!/usr/bin/env bash
# Install the bundled Amp plugin for this user. Compatible with macOS bash 3.2.
set -euo pipefail

FORCE=0
case "${1:-}" in
    --force) FORCE=1 ;;
    '') ;;
    *) echo "Usage: $0 [--force]" >&2; exit 2 ;;
esac
if [ "$#" -gt 1 ]; then
    echo "Usage: $0 [--force]" >&2
    exit 2
fi

SOURCE_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
DEST_DIR="${HOME:?HOME must be set}/.config/amp/plugins"
DEST="$DEST_DIR/revdiff.ts"

if [ -L "$DEST" ] || { [ -e "$DEST" ] && [ ! -f "$DEST" ]; }; then
    echo "Refusing to replace a symlink or non-regular file: $DEST" >&2
    exit 1
fi
if [ -f "$DEST" ] && ! cmp -s "$SOURCE_DIR/revdiff.ts" "$DEST" && [ "$FORCE" -ne 1 ]; then
    echo "A different plugin exists at $DEST; rerun with --force to replace it." >&2
    exit 1
fi

mkdir -p "$DEST_DIR"
TEMP=$(mktemp "$DEST_DIR/.revdiff-install.XXXXXX")
trap 'rm -f "$TEMP"' EXIT
cp "$SOURCE_DIR/revdiff.ts" "$TEMP"
chmod 644 "$TEMP"
mv -f "$TEMP" "$DEST"
printf 'Installed %s\nReload Amp plugins or restart Amp to activate it.\n' "$DEST"
