#!/usr/bin/env bash
# Runs the agent in agent.yaml on a case folder. With no folder, it copies the
# synthetic sample case to a temp folder. It prepares the case (ingest,
# extract, describe, index), runs the agent with a task, and then checks the
# result: a note in notes/ and no change to the originals.
#
# Usage: ./run.sh [case-folder] ["task"]
# Environment: PROFILE (small or large), MODEL (the agent model on Docker
# Model Runner), OPENARCH (the openarch binary). openarch-docs must be on PATH
# or next to openarch.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
profile=${PROFILE:-large}
case "$profile" in
small) default_model=ai/qwen2.5 ;;
large) default_model=ai/glm-4.7-flash:31B ;;
*) echo "PROFILE must be small or large" >&2; exit 2 ;;
esac
model=${MODEL:-$default_model}
openarch=$(command -v "${OPENARCH:-openarch}") || { echo "openarch not found; set OPENARCH to the binary" >&2; exit 3; }
export PATH="$(dirname "$openarch"):$PATH"
command -v openarch-docs >/dev/null || { echo "openarch-docs not found on PATH" >&2; exit 3; }
docker agent version >/dev/null 2>&1 || { echo "Docker Agent not found: https://github.com/docker/docker-agent" >&2; exit 3; }
docker model status >/dev/null 2>&1 || { echo "Docker Model Runner is not running: enable it in Docker Desktop" >&2; exit 3; }
for m in "$model" ai/gemma3:4B-Q4_K_M ai/mxbai-embed-large; do
	docker model inspect "$m" >/dev/null 2>&1 || { echo "model $m not found; pull it with: docker model pull $m" >&2; exit 3; }
done

if [ $# -ge 1 ] && [ -d "$1" ]; then
	casedir=$(cd "$1" && pwd)
	shift
else
	casedir=$(mktemp -d)/sample-case
	cp -R "$here/sample/case" "$casedir"
	# The chat export is a zip, as WhatsApp writes it.
	(cd "$here/sample/whatsapp" && zip -q -j "$casedir/WhatsApp Chat - Instalador Ejemplo.zip" _chat.txt *.jpg)
fi
task=${1:-"¿Quién envió el plano de la centralización y la foto del cuarto de contadores, y qué portal muestran? Escribe una nota de estado en notes/estado.md con las fuentes de cada dato."}
export OPENARCH_DOCS_CASE=$casedir OPENARCH_DOCS_PROFILE=$profile

echo "== case: $casedir"
echo "== prepare"
openarch docs ingest | tail -1
openarch docs extract | tail -1
openarch docs describe | tail -1
openarch docs index | tail -1
openarch docs skill install

echo "== agent ($model)"
echo "== task: $task"
# Docker Agent loads skills from .claude/skills in the working folder.
cd "$casedir"
docker agent run "$here/agent.yaml" --exec --safety autonomous --model "root=dmr/$model" "$task"

# Do not trust the report of the agent: check the folder directly.
echo
echo "== check"
openarch docs verify >/dev/null || { echo "FAIL: an original or a derived file changed" >&2; exit 1; }
notes=0
[ -d "$casedir/notes" ] && notes=$(find "$casedir/notes" -type f | wc -l | tr -d ' ')
[ "$notes" -gt 0 ] || { echo "FAIL: the agent wrote no note" >&2; exit 1; }
echo "notes written: $notes"
find "$casedir/notes" -type f -exec sh -c 'echo "--- $1"; cat "$1"' _ {} \;
echo "PASS"
