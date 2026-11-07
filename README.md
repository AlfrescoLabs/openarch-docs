# openarch-docs

A plugin for the [openarch CLI](https://github.com/AlfrescoLabs/openarch-cli) to work with a folder of case documents: emails, chat exports, PDFs, scans and images.

It stores the files in OpenArch, extracts their text, and answers questions about them with local models on Docker Model Runner. Nothing goes to the cloud. Every answer cites the file it comes from.

Status: early release. Commands and file formats can still change.

## Requirements

- Docker with [Docker Model Runner](https://docs.docker.com/ai/model-runner/).
- The [`openarch` CLI](https://github.com/AlfrescoLabs/openarch-cli) 0.5.0 or later, and a running OpenArch (`openarch up`). OpenArch is the store of record: `sync` uploads the case folder to it, and `reindex` brings it back.

## Install

Download the archive for your system from the [releases page](https://github.com/AlfrescoLabs/openarch-docs/releases): Linux, macOS and Windows, on amd64 or arm64. Put `openarch-docs` on your `PATH`, next to `openarch`.

```
VERSION=0.1.0
OS=linux        # or darwin, windows
ARCH=amd64      # or arm64
curl -LO "https://github.com/AlfrescoLabs/openarch-docs/releases/download/v$VERSION/openarch-docs_${VERSION}_${OS}_${ARCH}.tar.gz"
curl -LO "https://github.com/AlfrescoLabs/openarch-docs/releases/download/v$VERSION/checksums.txt"
sha256sum --ignore-missing -c checksums.txt     # on macOS: shasum -a 256 --ignore-missing -c checksums.txt
tar xzf "openarch-docs_${VERSION}_${OS}_${ARCH}.tar.gz"
sudo mv openarch-docs /usr/local/bin/
openarch docs version
```

On Windows, download the `.zip` archive and put `openarch-docs.exe` in a folder on your `PATH`. The binaries are not signed with an Apple or Microsoft certificate: when a browser downloaded the file, run `xattr -d com.apple.quarantine openarch-docs` once on macOS.

`checksums.txt` is signed with [cosign](https://docs.sigstore.dev/) by the release workflow (keyless, with the GitHub Actions identity), and each archive has an SBOM (`.sbom.json`, SPDX):

```
curl -LO "https://github.com/AlfrescoLabs/openarch-docs/releases/download/v$VERSION/checksums.txt.sigstore.json"
cosign verify-blob checksums.txt \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity "https://github.com/AlfrescoLabs/openarch-docs/.github/workflows/release.yml@refs/tags/v$VERSION" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## Models

Pick the profile for your computer with `--profile` or `OPENARCH_DOCS_PROFILE`. The default is `small`.

| Profile | RAM | Chat (`ask`) | Vision (`describe`, `look`) | Embeddings (`index`, `search`) |
| --- | --- | --- | --- | --- |
| `small` | 16 GB | `ai/qwen2.5` | `ai/gemma3:4B-Q4_K_M` | `ai/mxbai-embed-large` |
| `large` | 32 GB | `ai/glm-4.7-flash:31B` | `ai/gemma3:4B-Q4_K_M` | `ai/mxbai-embed-large` |

Pull the models of your profile once:

```
docker model pull ai/qwen2.5              # small; for large: ai/glm-4.7-flash:31B
docker model pull ai/gemma3:4B-Q4_K_M
docker model pull ai/mxbai-embed-large
```

The first `extract` builds a local Docker image with the PDF and OCR tools. It runs with no network.

## Try it

[examples/demo](examples/demo) is a synthetic case in English: forwarded emails, a scanned letter, a cable plan, photos, a card receipt and a WhatsApp export. From a clone of this repository:

```
examples/demo/prepare.sh /tmp/ev-charger
cd /tmp/ev-charger
openarch docs ingest
openarch docs extract
openarch docs describe
openarch docs index
openarch docs ask --new "Who took the photos on page 2 of the connection offer?"
openarch docs ask --continue "Where was Tom Hughes that day?"
openarch docs timeline
```

## Use it on your own case

```
openarch docs init ~/cases/garage && cd ~/cases/garage
openarch docs add ~/Downloads/*.eml ~/Downloads/"WhatsApp Chat - Installer.zip"
openarch docs ingest
openarch docs extract
openarch docs describe
openarch docs index
openarch docs ask "Where is the meter room?"
openarch docs sync
openarch docs verify
```

`openarch docs status` names the next step. Questions and answers work in English, Spanish, French and Portuguese: `ask` answers in the language of the question.

## Commands

| Command | What it does |
| --- | --- |
| `init` | Makes the layout of a case folder: `references/`, `notes/`, `AGENTS.md` and the Agent Skill |
| `add` | Copies files or pasted text into the case folder, and records where they came from |
| `ingest` | Records each original with its SHA-256, and unpacks emails (attachments, inline images, attached and quoted messages) and WhatsApp exports into `extracted/` |
| `extract` | Gets the text of every PDF and image, with OCR for scans |
| `describe` | Writes a text for each photo, plan and image page with the vision model, so search finds them |
| `index` | Builds the local search index: full text search and embeddings |
| `search` | Finds texts by words and by meaning, and cites file and page |
| `ask` | Answers a question with the chat model and cites a source for every fact; `--new` and `--continue` for a conversation |
| `look` | Asks the vision model one question about one or more images |
| `timeline` | Lists emails, quoted messages and chat messages in time order |
| `note` | Writes notes and drafts in `notes/` |
| `status` | Shows what is left to do, and the next command |
| `collect` | Copies files to attach into one folder, with their real names |
| `sync`, `reindex` | Upload the case folder to OpenArch, with the provenance of each file; rebuild it from OpenArch |
| `verify` | Checks the originals and derived files against their SHA-256 |
| `eval` | Asks a set of questions and scores the answers |
| `mcp`, `skill` | An MCP server and an Agent Skill for AI agents |

Every command has `--help` and `--output json`.

## Agents

`openarch docs mcp` is an MCP server with tools to search and read the case folder, look at its images, and write notes in `notes/`. `openarch docs skill install` adds an Agent Skill to the case folder. [examples/agent](examples/agent) runs a Docker Agent with both on a synthetic sample case in Spanish.

## Design

[docs/design.md](docs/design.md) explains the decisions, the pipeline and the limits.

## Build

```
make build
go test ./...
```

## License

Apache License 2.0.
