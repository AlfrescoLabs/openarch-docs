# openarch-docs

A plugin for the [openarch CLI](https://github.com/AlfrescoLabs/openarch-cli) to work with a folder of case documents: emails, chat exports, PDFs, scans and images.

It stores the files in OpenArch, extracts their text, and answers questions about them with local models on Docker Model Runner. Nothing goes to the cloud.

Status: early development. `ingest`, `extract`, `index`, `search`, `verify` and `version` work today.

## Requirements

- Docker with Docker Model Runner.
- The `openarch` CLI and a running OpenArch (`openarch up`).

## Use

Put `openarch-docs` on your `PATH`. Then:

```
cd my-case-folder
openarch docs ingest
openarch docs extract
openarch docs index
openarch docs search "cuarto de contadores"
openarch docs verify
```

`ingest` records each original with its SHA-256 and unpacks emails (body, attachments, inline images, attached emails) and WhatsApp exports into `extracted/`. `extract` gets the text of every PDF and image, with OCR in English, Spanish, French and Portuguese for scans. It runs the programs in a local Docker image with no network. `index` builds a local search index with full text search and embeddings from a local model. `search` finds texts by words and by meaning, and cites file and page. `verify` checks that nothing changed.

## Build

```
make build
go test ./...
```

## License

Apache License 2.0.
