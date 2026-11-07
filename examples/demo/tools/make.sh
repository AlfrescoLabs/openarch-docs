#!/usr/bin/env bash
# Makes the files of the demo case again: the images, the scanned PDF and the
# emails in case/, and the photos of the WhatsApp chat in whatsapp/. Needs
# ImageMagick, img2pdf, python3 and the macOS fonts.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
demo=$(dirname "$here")
assets=$(mktemp -d)
trap 'rm -rf "$assets"' EXIT

"$here/make_images.sh" "$assets"
python3 -I "$here/make_emails.py" "$assets" "$demo/case"
cp "$assets"/0000000*-PHOTO-*.jpg "$demo/whatsapp/"
