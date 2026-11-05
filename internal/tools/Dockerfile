# Tools image of openarch-docs: PDF text, page rendering and OCR. The plugin
# builds it locally on first use and runs it with no network.
FROM debian:trixie-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends poppler-utils tesseract-ocr \
    tesseract-ocr-eng tesseract-ocr-spa tesseract-ocr-fra tesseract-ocr-por \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --uid 10001 --create-home tools
USER tools
WORKDIR /out
