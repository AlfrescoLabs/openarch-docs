---
name: openarch-docs
description: Work with a case folder of emails, chat exports, PDFs, scans and photos through the openarch-docs tools - search with citations, read files with their provenance, look at images with the local vision model, build timelines and write notes and email drafts. Use it when a task asks about the facts, documents, photos or dates of a case folder, or asks for a status note or a reply.
---

# Case folders with openarch-docs

A case folder holds original documents. `openarch docs` has already unpacked them: email bodies, attachments, chat messages and photos, PDF pages, OCR and image descriptions. Each file has provenance: the email or chat message that sent it, the page, and how its text was made.

## Rules

- Every fact comes from a tool result. Never invent a fact, a date, a name or a file.
- Cite each fact with the citation (`cite`) or the path of its source. In notes and drafts, put the file path in parentheses after each fact.
- Say how a file arrived (by email or chat, from whom, when) only as `files` or the provenance of `read` shows it.
- Keep facts apart from interpretations. Write "the sources show" for facts and "this suggests" for interpretations.
- Do not say that a person or a company is at fault unless a source says so.
- Sources with role `note` are earlier notes: interpretations, not evidence. Sources with role `reference` are laws or rules, not facts of the case.
- An email often quotes other emails. Say who wrote each quoted part.
- You cannot see images. Use `look` for photos, plans and PDF pages, and say that the vision model can be wrong.
- The tools are offline. When a fact needs a document that is not in the folder (a law, a cadastre map), say which document to add: laws go in `references/`, other files in the case folder.
- Write only with `note_write` and `collect`, in `notes/`. Drafts go in `notes/drafts/`. Never change case documents.
- Answer and write in the language of the user. Keep accents and special letters (ñ, ç, ã).

## Tools

| Need | Tool |
| --- | --- |
| Find texts about a topic | `search` (add `roles: ["case"]` to leave notes out) |
| Read a whole email, page or description | `read` with the `path` of a hit |
| Find attachments, photos, page images, and who sent them | `files` with `kind` (`attachment`, `media`, `page-image`, `description`) |
| Dates and order of events | `timeline` |
| What a photo or plan shows, or compare images | `look` with paths, or `<pdf path>#<page>` |
| Save a status note, timeline or email draft | `note_write` |
| Gather the files to attach to an email | `collect` (copies to `notes/outbox/<date>/` with their real names) |
| Know if the case is ready, or what to run | `status` |
| Check that the originals did not change | `verify` |

## Work flow

1. `search` for the topic. Then `read` the best hits: hits are pieces, files have the full text.
2. For "who sent X": `files` gives the sender and date of each attachment and photo, and `read` on a `message.txt` gives the email with its quoted forwards.
3. For photos and plans: find the page images with `files` (`kind: page-image`, `images` above 0 means photos or plans), then `look`.
4. Give a short answer with citations. For a reply or a note, write it with `note_write` and give its path.

## Commands

The same work from a shell, for people:

- `openarch docs init <folder>` makes a case folder. `openarch docs add <file>` or `add --text` (pasted text) adds documents with where they came from; `add --reference` for laws.
- `openarch docs ingest`, `extract`, `describe`, `index`: prepare a case folder, in this order. `openarch docs status` tells what is left.
- `openarch docs search "..."`, `ask "..."`, `timeline`, `look <images> -q "..."`.
- `openarch docs note write drafts/reply.md < text.md`, `openarch docs collect <files>`.
- `openarch docs sync` uploads the case to OpenArch; `reindex` rebuilds it from OpenArch.
- Every command takes `--output json`. Run `openarch docs <command> --help` for flags.
