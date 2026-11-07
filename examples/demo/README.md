# Demo case: an EV charger in a shared garage

A synthetic case folder for demos and for checking answer quality. Maya Collins wants an electric vehicle charger at her parking space 18 in the garage of Linden Court. Her installer, the network operator and the residents' association do not agree on the cable route.

All people, companies, places, numbers and documents are invented. Mail addresses use the `example` domains.

## What is in it

| File | Shape |
| --- | --- |
| `Fwd_ Connection offer EVN-48213.eml` | Gmail forward with a scanned 2-page PDF (OCR): a letter, and a page of survey photos |
| `Cable route for space 18.eml` | Email with a cable route plan (PNG) |
| `Re_ Cable route for space 18.eml` | Reply with two quoted messages, ISO-8859-1 quoted-printable, HTML with an inline logo |
| `Fwd_ Payment receipt - quote Q-2026-117.eml` | Forward as attachment (`message/rfc822`) with a photo of a card receipt |
| `Charger at space 18 - notice.eml` | Email with a land register screenshot |
| `Re_ Charger at space 18 - notice.eml` | Reply with a quoted message |
| `Fwd_ Re_ Charger at space 18 - notice.eml` | Forward with almost no text of its own |
| `whatsapp/` | iOS WhatsApp export with two photos; `prepare.sh` zips it |
| `references/example-garage-rules.txt` | An invented reference text |

`questions.txt` has 12 questions with expected words and key facts, for `openarch docs eval`.

## Use it

```
./prepare.sh /tmp/ev-charger
export OPENARCH_DOCS_CASE=/tmp/ev-charger
openarch docs ingest
openarch docs extract
openarch docs describe
openarch docs index
openarch docs ask "Who took the photos on page 2 of the connection offer?"
openarch docs eval questions.txt
```

`eval` writes the totals: keyword checks (pass, partial, fail) and the judge score. The judge is the chat model of the profile. Give the same `--judge-model` to compare two profiles.

## Make the files again

`tools/make.sh` draws the images and the scanned PDF with ImageMagick and img2pdf, and writes the emails with Python. It needs the macOS fonts. Two runs give the same bytes.
