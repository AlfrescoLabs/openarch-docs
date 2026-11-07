# openarch-docs

A plugin for the [openarch CLI](https://github.com/AlfrescoLabs/openarch-cli) to work with a folder of case documents: emails, chat exports, PDFs, scans and images.

It stores the files in OpenArch, extracts their text, and answers questions about them with local models on Docker Model Runner. Nothing goes to the cloud.

Status: early development. `init`, `add`, `status`, `collect`, `eval`, `ingest`, `extract`, `describe`, `index`, `search`, `ask`, `timeline`, `look`, `sync`, `reindex`, `note`, `mcp`, `skill`, `verify` and `version` work today.

## Requirements

- Docker with Docker Model Runner.
- The `openarch` CLI (a release with `documents upload --replace`) and a running OpenArch (`openarch up`).

## Use

Put `openarch-docs` on your `PATH`. Then:

```
openarch docs init my-case-folder && cd my-case-folder
openarch docs add ~/Downloads/*.eml
openarch docs ingest
openarch docs extract
openarch docs describe
openarch docs index
openarch docs search "cuarto de contadores"
openarch docs ask "¿Quién aportó las fotos de la solicitud?"
openarch docs timeline
openarch docs sync
openarch docs verify
```

`ingest` records each original with its SHA-256 and unpacks emails (body, attachments, inline images, attached emails) and WhatsApp exports into `extracted/`. `extract` gets the text of every PDF and image, with OCR in English, Spanish, French and Portuguese for scans. It runs the programs in a local Docker image with no network. `describe` writes a text for each photo, plan and image page with a local vision model, so search finds them. `look` asks the vision model about several images at once. `index` builds a local search index with full text search and embeddings from a local model. `search` finds texts by words and by meaning, and cites file and page. `ask` answers a question in its language with a local chat model and cites a source for every fact. `timeline` lists emails, quoted forwards and chat messages in time order. `sync` uploads the originals and derived files to OpenArch, each with its provenance in the description; `reindex` rebuilds the case folder and the index from OpenArch. `verify` checks that nothing changed.

## Build

```
make build
go test ./...
```

## License

Apache License 2.0.

## Agents

`openarch docs mcp` is an MCP server with tools to search and read the case folder, look at its images, and write notes in `notes/`. `openarch docs skill install` adds an Agent Skill to the case folder. [examples/agent](examples/agent) runs a Docker Agent with both on a synthetic sample case. [examples/demo](examples/demo) is a larger synthetic case in English, with scans, a plan, photos and a chat export.
