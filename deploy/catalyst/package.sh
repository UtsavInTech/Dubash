#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
output="${1:-/private/tmp/dubash-catalyst.zip}"
folder="$(mktemp -d)"
trap 'rm -rf "$folder"' EXIT
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o "$folder/dubash" ./cmd/dubash
cp deploy/catalyst/launcher.py "$folder/launcher.py"
cp rooms.yaml "$folder/rooms.yaml"
if [[ -f data/events.yaml ]]; then cp data/events.yaml "$folder/events.yaml"; fi
mkdir -p "$folder/samples"
cp samples/*.wav "$folder/samples/"
python3 - "$folder" "$output" <<'PY'
import pathlib, sys, zipfile
root = pathlib.Path(sys.argv[1])
with zipfile.ZipFile(sys.argv[2], 'w', zipfile.ZIP_DEFLATED) as out:
    for item in sorted(root.rglob('*')):
        if item.is_file(): out.write(item, item.relative_to(root))
print(sys.argv[2])
PY
