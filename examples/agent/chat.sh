#!/usr/bin/env bash
# A conversation with the agent about a case folder. The conversation is kept
# in <case>/.agent/session.db, so each message continues the last one, as in
# a chat. With no message, it opens the Docker Agent terminal UI on the same
# conversation. Prepare the case first (run.sh, or ingest, extract, describe,
# index).
#
# Usage: ./chat.sh <case-folder> ["message"]
#        ./chat.sh <case-folder> --new ["message"]   (start a new conversation)
# Environment: PROFILE (small or large), MODEL, OPENARCH, as in run.sh.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
[ $# -ge 1 ] && [ -d "$1" ] || { echo "usage: $0 <case-folder> [--new] [\"message\"]" >&2; exit 2; }
casedir=$(cd "$1" && pwd)
shift
profile=${PROFILE:-large}
case "$profile" in
small) default_model=ai/qwen2.5 ;;
large) default_model=ai/glm-4.7-flash:31B ;;
*) echo "PROFILE must be small or large" >&2; exit 2 ;;
esac
model=${MODEL:-$default_model}
openarch=$(command -v "${OPENARCH:-openarch}") || { echo "openarch not found; set OPENARCH to the binary" >&2; exit 3; }
export PATH="$(dirname "$openarch"):$PATH" OPENARCH_DOCS_CASE=$casedir OPENARCH_DOCS_PROFILE=$profile

mkdir -p "$casedir/.agent"
db=$casedir/.agent/session.db
session=case
if [ "${1:-}" = "--new" ]; then
	shift
	session=case-$(date +%Y%m%d-%H%M%S)
	echo "$session" > "$casedir/.agent/session"
elif [ -f "$casedir/.agent/session" ]; then
	session=$(cat "$casedir/.agent/session")
fi

openarch docs skill install >/dev/null
cd "$casedir"
if [ $# -eq 0 ]; then
	exec docker agent run "$here/agent.yaml" --session-db "$db" --session "$session" --model "root=dmr/$model"
fi
docker agent run "$here/agent.yaml" --exec --safety autonomous --session-db "$db" --session "$session" --model "root=dmr/$model" "$1"
