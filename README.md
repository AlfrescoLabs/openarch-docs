# openarch-docs

A plugin for the [openarch CLI](https://github.com/AlfrescoLabs/openarch-cli) to work with a folder of case documents: emails, chat exports, PDFs, scans and images.

It stores the files in OpenArch, extracts their text, and answers questions about them with local models on Docker Model Runner. Nothing goes to the cloud.

Status: early development. `ingest`, `verify` and `version` work today.

## Requirements

- Docker with Docker Model Runner.
- The `openarch` CLI and a running OpenArch (`openarch up`).

## Use

Put `openarch-docs` on your `PATH`. Then:

```
cd my-case-folder
openarch docs ingest
openarch docs verify
```

`ingest` records each original with its SHA-256 and unpacks emails (body, attachments, inline images, attached emails) and WhatsApp exports into `extracted/`. `verify` checks that nothing changed.

## Build

```
make build
go test ./...
```

## License

Apache License 2.0.
