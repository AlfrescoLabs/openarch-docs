# openarch-docs

A plugin for the [openarch CLI](https://github.com/AlfrescoLabs/openarch-cli) to work with a folder of case documents: emails, chat exports, PDFs, scans and images.

It stores the files in OpenArch, extracts their text, and answers questions about them with local models on Docker Model Runner. Nothing goes to the cloud.

Status: early development. Only `version` works today.

## Requirements

- Docker with Docker Model Runner.
- The `openarch` CLI and a running OpenArch (`openarch up`).

## Use

Put `openarch-docs` on your `PATH`. Then:

```
openarch docs version
openarch plugin list
```

## Build

```
make build
go test ./...
```

## License

Apache License 2.0.
