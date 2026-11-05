# Design

`openarch-docs` is one Go binary. It runs as the `openarch docs` plugin of the openarch CLI, or on its own.

## Rules

1. **Local models only.** Chat, vision and embeddings run on Docker Model Runner. No cloud calls.
2. **Offline.** The plugin never downloads documents. Law texts and screenshots are files the user adds to the case folder.
3. **OpenArch is required.** It is the store of record for originals and derived files. The local index is a cache that can be rebuilt from OpenArch.
4. **Originals never change.** Every original has a SHA-256 checksum. Every derived file records its source file and, when it applies, the attachment name and the message headers.
5. **Only Docker on the host.** Text extraction, page rendering and OCR (Spanish and English) run in the plugin tools image with no network.
6. **Same output contract as openarch.** Every command supports `--output json`. Results go to stdout, logs to stderr. In JSON mode errors also go to stdout. Exit codes match openarch.
7. **Answers cite sources.** Every fact in an answer points to a file and a page. Case facts, reference texts (law) and notes (interpretations) are cited apart.

## Plugin contract

openarch runs `openarch-docs` with the arguments after `docs`. Global flags given to openarch before `docs` arrive as environment variables, which this binary reads as flag defaults:

| Variable | Flag |
| --- | --- |
| `OPENARCH_OUTPUT` | `--output` |
| `OPENARCH_NON_INTERACTIVE` | `--non-interactive` |
| `OPENARCH_VERBOSE` | `--verbose` |
| `NO_COLOR` | `--no-color` |

`OPENARCH_WORKSPACE` and `OPENARCH_CONTEXT` select the OpenArch workspace and connection. The plugin does not read them itself: it runs `openarch` (`$OPENARCH_BIN`, else `openarch` from `PATH`), which reads them. Passwords never reach the plugin.

The case folder is `--case`, then `OPENARCH_DOCS_CASE`, then the current folder.

## Model profiles

| Profile | Agent (tool calls) | Vision | Embeddings | RAM |
| --- | --- | --- | --- | --- |
| `small` | `ai/qwen2.5` (7B) | `ai/gemma3:4B-Q4_K_M` | `ai/mxbai-embed-large` | 16 GB |
| `large` | `glm-4.7-flash:31B` | `ai/gemma3:4B-Q4_K_M` | `ai/mxbai-embed-large` | 32 GB |

gemma3 reads Spanish scans well but cannot do tool calls on Docker Model Runner, so it is used only for vision. The plugin keeps one large model in memory at a time.

## Case folder

| Path | Content |
| --- | --- |
| any file outside the folders below | Original case documents. Never changed. |
| `references/` | Reference texts the user adds, such as laws. Originals with role `reference`. |
| `notes/` | Notes and drafts. Interpretations, not sources. |
| `extracted/manifest.json` | Every original (path, SHA-256, size, kind, role) and every derived file (path, SHA-256, source, provenance). |
| `extracted/<name>-<hash8>/` | Derived files of one original. `<hash8>` is the start of its SHA-256, so a changed original gets a new folder. |

Hidden files and agent files (`AGENTS.md`, `CLAUDE.md`) are not originals. Files in `extracted/` that are not in the manifest are left alone.

Derived files of an email: `message.txt` (headers and body text), `message.html`, `attachments/`, `inline/`, and `message-N/` for each attached email with its own `message.eml`. Provenance in the manifest:

- `message`: the headers of the email that holds the file.
- `chain`: the emails around it, outermost first, for attached emails.
- `forwarded`: the forwarded or quoted messages found in the body text (marker lines, or `From:`/`De:` blocks, in English and Spanish).
- `part` and `attachment`: the MIME part number and the file name the sender gave.

Derived files of a WhatsApp export: `chat.txt`, `messages.json` and `media/`. Each media file records who sent it and when. The date order (day or month first) comes from the export; when no date shows it, `messages.json` keeps the time as written and has no ISO time.
