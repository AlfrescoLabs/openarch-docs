# Design

`openarch-docs` is one Go binary. It runs as the `openarch docs` plugin of the openarch CLI, or on its own.

## Rules

1. **Local models only.** Chat, vision and embeddings run on Docker Model Runner. No cloud calls.
2. **Offline.** The plugin never downloads documents. Law texts and screenshots are files the user adds to the case folder.
3. **OpenArch is required.** It is the store of record for originals and derived files. The local index is a cache that can be rebuilt from OpenArch.
4. **Originals never change.** Every original has a SHA-256 checksum. Every derived file records its source file and, when it applies, the attachment name and the message headers.
5. **Only Docker on the host.** Text extraction, page rendering and OCR run in the plugin tools image with no network.
6. **Same output contract as openarch.** Every command supports `--output json`. Results go to stdout, logs to stderr. In JSON mode errors also go to stdout. Exit codes match openarch.
7. **Languages.** English, Spanish, French and Portuguese. The words that mail clients and chat exports write (forward markers, quoted headers, attachment lines) and the OCR codes live in one table, `internal/lang`. Parsers match every language at once, so one case folder can mix them. A new language is one new entry.
8. **Answers cite sources.** Every fact in an answer points to a file and a page. Case facts, reference texts (law) and notes (interpretations) are cited apart.

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

Hidden files and agent files (`AGENTS.md`, `CLAUDE.md`) are not originals. Each original records the parser version (`parser`) that wrote its derived files. When a release parses emails or zips better, `ingest` writes their derived files again and keeps the `extract` results of files that still exist.

Email text is cleaned: HTML entities in plain text parts are decoded, and text labeled ISO-8859-1 that uses Windows-1252 characters (en dash, euro sign, curly quotes) is read as Windows-1252, as browsers do. Files in `extracted/` that are not in the manifest are left alone.

Derived files of an email: `message.txt` (headers and body text), `message.html`, `attachments/`, `inline/`, and `message-N/` for each attached email with its own `message.eml`. Provenance in the manifest:

- `message`: the headers of the email that holds the file.
- `chain`: the emails around it, outermost first, for attached emails.
- `forwarded`: the forwarded or quoted messages found in the body text (marker lines such as "Forwarded message" or "Mensagem encaminhada", or a `From:`/`De :` block), in every supported language.
- `part` and `attachment`: the MIME part number and the file name the sender gave.

Derived files of a WhatsApp export (iOS or Android, in any supported language): `chat.txt`, `messages.json` and `media/`. Each media file records who sent it and when. The date order (day or month first) comes from the export; when no date shows it, `messages.json` keeps the time as written and has no ISO time.

## Extract

`extract` reads every PDF and image: originals, and files that `ingest` unpacked (attachments, inline images, chat media, zip files). Results go next to the file: `extracted/<name>-<hash8>/` for an original, `<file>.extract/` for an unpacked file.

| File | Kind | Content |
| --- | --- | --- |
| `page-NNN.txt` | `page-text` | PDF text layer of the page (method `text-layer`). Only when the page has text. |
| `page-NNN.png` | `page-image` | Image of the page, for vision models and for people. |
| `page-NNN.ocr.txt` | `page-ocr` | OCR of the page, when the text layer has fewer than `--min-text` letters and digits (method `ocr`). |
| `ocr.txt` | `image-ocr` | OCR of an image. |

Each entry has `from` (the PDF or image), `page`, and `images`: the number of images that cover at least 10% of the page (size from `pdfimages -list` pixels and resolution, page size from `pdfinfo`), so logos and letterheads do not count. Pages with images are listed in the result, because photos and plans carry facts that the text does not.

Tools: poppler (`pdfinfo`, `pdftotext`, `pdfimages`, `pdftoppm`) and tesseract with `eng`, `spa`, `fra` and `por`. They run in the tools image, built locally from `internal/tools/Dockerfile` on first use (tag `openarch-docs-tools:<hash of the Dockerfile>`). Each run has no network, no capabilities, and only the input file (read only) and a work folder mounted. A PDF takes two runs: text layer and image list, then page images and OCR. `--tools local` runs the same programs from `PATH`.

HEIC and WebP files are reported as unsupported: tesseract cannot read them.

## Index and search

`index` keeps `extracted/index.sqlite` (pure Go SQLite). It is a cache: deleting it only costs a rebuild.

- Texts: email bodies (`message.txt`), chat messages (one line per message, with time and sender), page texts and OCR from `extract`, text originals, and every `.md` and `.txt` file in `notes/`.
- Each text is split into chunks of about 800 characters at paragraph, line or sentence ends, with 120 characters of overlap. A change of these settings splits every text again. The rare chunk that is still longer than the 512 tokens of mxbai is cut for its embedding only. Chunks with fewer than 20 letters and digits (OCR noise of photos) are dropped.
- Each chunk keeps its citation (file or attachment name, email subject and date, page, "(OCR)"), its path, the original, the role (`case`, `reference`, `note`), the sender and the date.
- Full text search: FTS5 with `unicode61 remove_diacritics 2`, so queries without accents find accented words. Short common words of each supported language (`StopWords` in `internal/lang`) are left out of queries; question words such as "donde" or "where" stay.
- Embeddings: the embed model of the profile, with the citation before the chunk text. Queries get the mxbai instruction prefix. Vectors are normalized and compared by dot product over all chunks (case folders are small).
- Only texts whose SHA-256 changed are split again. Chunks without embeddings (after `--no-embed`, or after a change of embedding model) get them on the next run.

`search` merges the BM25 list and the embedding list with reciprocal rank fusion (k = 60). Each hit says which search found it (`text`, `meaning`).

Models run on Docker Model Runner at `--model-url` (default `http://localhost:12434/engines/v1`, env `OPENARCH_DOCS_MODEL_URL`). `--profile` (env `OPENARCH_DOCS_PROFILE`) picks the models.

## Ask

`ask` searches the index for the question (case documents and references; notes only with `--with-notes`), and gives the best chunks to the chat model of the profile as numbered sources. Each source shows its role (`case`, `reference`, `note`), its citation and its sender.

The system message asks the model to answer in the language of the question, to cite a source for every fact as `[n]`, to say plainly when the sources do not hold the answer, to keep facts apart from interpretations, to treat notes as interpretations and references as rules, and to blame nobody without a source. The model makes no tool calls, so small models work.

The result lists the sources, marks the cited ones, and lists cited numbers that are not sources (`unknown`): a sign that the model made up a citation.

## Timeline

`timeline` needs no model. It lists, oldest first:

- each email (`message.txt` headers),
- each forwarded or quoted message in an email body (`forwarded` in the manifest),
- each chat message (time, sender, text or attachment name).

Quoted dates are read in every supported language with the month names in `internal/lang` ("mié, 29 oct 2026 a las 18:00", "Fri, Oct 30, 2026 at 5:51 PM", "29 de outubro de 2026 18:00", "30/10/2026 7:51"; numeric dates are day first unless the month would pass 12). Times stay in the time zone of the source. A message that is an email in one file and a quoted forward in another is listed once, as the email.
