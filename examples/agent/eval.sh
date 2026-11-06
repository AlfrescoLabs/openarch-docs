#!/usr/bin/env bash
# Runs the question set of the synthetic sample case through openarch docs
# eval, as a regression check of answer quality. It copies the sample case to
# a temp folder, prepares it (ingest, extract, describe, index), and writes
# the report to eval-report.json in the current folder.
#
# Usage: ./eval.sh [extra eval flags]
# Environment: PROFILE (small or large, default large).
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
export OPENARCH_DOCS_PROFILE=${PROFILE:-large}
command -v openarch-docs >/dev/null || { echo "openarch-docs not found on PATH" >&2; exit 3; }
docker model status >/dev/null 2>&1 || { echo "Docker Model Runner is not running: enable it in Docker Desktop" >&2; exit 3; }

casedir=$(mktemp -d)/sample-case
cp -R "$here/sample/case" "$casedir"
(cd "$here/sample/whatsapp" && zip -q -j "$casedir/WhatsApp Chat - Instalador Ejemplo.zip" _chat.txt *.jpg)
export OPENARCH_DOCS_CASE=$casedir

echo "== case: $casedir"
openarch-docs ingest | tail -1
openarch-docs extract | tail -1
openarch-docs describe | tail -1
openarch-docs index | tail -1
openarch-docs eval "$here/sample/questions.txt" "$@" -o json > eval-report.json
echo "== report: eval-report.json"
grep -E '^  "(pass|partial|fail|judgeScore|judgeMax)":' eval-report.json
