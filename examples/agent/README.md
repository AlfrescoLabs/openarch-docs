# Case folder agent

A [Docker Agent](https://github.com/docker/docker-agent) that answers questions about a case folder and writes notes, with local models on Docker Model Runner.

The agent has no shell and no network. Its only tools come from `openarch docs mcp`: `search`, `read`, `files`, `timeline`, `look` (the vision model), `verify` and `note_write`. Every tool reads the case folder, except `note_write`, which writes only in `notes/`. The agent also loads the `openarch-docs` skill from the case folder.

## Run it

```
./run.sh                      # the synthetic sample case
./run.sh ~/cases/my-case "¿Qué falta para cerrar el caso? Escribe una nota en notes/pendiente.md."
```

For a conversation, use `chat.sh` on a prepared case folder. Each message continues the last one; the conversation is kept in `<case>/.agent/session.db`. With no message it opens the Docker Agent terminal UI on the same conversation, and `--new` starts a new one. At the start of a conversation the agent reads `status` and the newest notes, so the work of earlier conversations carries over.

```
./chat.sh ~/cases/my-case "¿Quién envió la foto del cuarto de contadores?"
./chat.sh ~/cases/my-case "¿Y qué portal dijo esa persona?"
./chat.sh ~/cases/my-case
```

`run.sh` prepares the case (`ingest`, `extract`, `describe`, `index`), installs the skill, runs the agent with the task, and then checks the folder itself: `verify` must pass and the agent must have written a note.

Requirements: Docker with Docker Model Runner, Docker Agent, `openarch` and `openarch-docs` on `PATH`, and the models of the profile:

```
docker model pull ai/glm-4.7-flash:31B
docker model pull ai/gemma3:4B-Q4_K_M
docker model pull ai/mxbai-embed-large
```

## Models

The agent needs a model that makes tool calls well. `PROFILE=large` (the default, about 32 GB of RAM) uses GLM 4.7 Flash; it completes the sample task and cites a file for each fact.

`PROFILE=small` (about 16 GB of RAM) uses Qwen 2.5 7B. It calls the search tools, but it often writes the note as text instead of calling `note_write`, and it can answer in English. On a 16 GB computer, use `openarch docs ask` for questions instead: it needs no tool calls.

`MODEL` picks another agent model on Docker Model Runner.

## The sample case

`sample/` is synthetic: a forwarded email with a scanned plan, an email thread with quoted replies, a WhatsApp export with a photo of a meter room, and an example law text in `references/`. The email and the chat disagree on the building entrance, so a good note reports the conflict.

The vision model and the agent can be wrong. Check important facts in the cited files.

## Check answer quality

`eval.sh` asks the questions in `sample/questions.txt` with `openarch docs ask` and scores the answers, as a regression check after a change to search or ask. It needs no agent and no tool calls.

```
./eval.sh                     # large profile
PROFILE=small ./eval.sh
./eval.sh --expand --rerank   # measure an optional retrieval step
```

It writes `eval-report.json` in the current folder and prints the totals: keyword checks (pass, partial, fail) and the judge score. Each question has `F:` lines with its key facts, and the judge model checks each one. Compare the totals with an earlier report: one point up or down is often model noise.
