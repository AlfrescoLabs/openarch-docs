#!/usr/bin/env bash
# Copies the demo case to a new folder and zips the WhatsApp chat, as a phone
# exports it. The folder is then ready for openarch docs ingest.
#
# Usage: ./prepare.sh <new-folder>
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
dst=${1:?usage: prepare.sh <new-folder>}
if [ -e "$dst" ]; then
  echo "$dst exists" >&2
  exit 2
fi
cp -R "$here/case" "$dst"
(cd "$here/whatsapp" && zip -q -j "$dst/WhatsApp Chat - Tom Hughes.zip" _chat.txt ./*.jpg)
echo "$dst"
