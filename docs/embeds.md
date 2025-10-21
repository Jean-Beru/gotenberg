# PDF Embeds Feature

## Overview

The PDF embeds feature allows you to embed files into a generated PDF without modifying the main PDF content. Files are embedded as file attachments that can be extracted by PDF readers.

## Usage

To embed files into a PDF, include them in your multipart form data request using the `embeds` field name:

```bash
curl -X POST http://localhost:3000/forms/chromium/convert/html \
  -F "files=@index.html" \
  -F "embeds=@document.pdf" \
  -F "embeds=@data.xml" \
  -F "embeds=@image.jpg"
```

### Supported Routes

The embeds feature is available on all PDF generation routes:

- **Chromium routes:**
  - `/forms/chromium/convert/html`
  - `/forms/chromium/convert/url`
  - `/forms/chromium/convert/markdown`

- **LibreOffice routes:**
  - `/forms/libreoffice/convert`

- **PDF engines routes:**
  - `/forms/pdfengines/merge`
  - `/forms/pdfengines/split`

### Multiple Embeds

You can embed multiple files by repeating the `embeds` field:

```bash
curl -X POST http://localhost:3000/forms/chromium/convert/html \
  -F "files=@index.html" \
  -F "embeds=@file1.pdf" \
  -F "embeds=@file2.xml" \
  -F "embeds=@file3.json"
```

## Implementation Details

### PDF Engine Support

Currently, only **pdfcpu** supports file embedding. Other PDF engines (qpdf, pdftk, exiftool, libreoffice-pdfengine) will return an error if embedding functionality is requested.

The default configuration uses pdfcpu for embeds:

```bash
--pdfengines-embed-engines=pdfcpu
```

### Processing Order

Embeds are added to the PDF in the following order during processing:

1. PDF generation (from HTML, URL, Markdown, or LibreOffice conversion)
2. Format conversion (if requested)
3. Metadata writing (if requested)
4. **File embedding** ← Embeds are added here
5. Encryption (if requested)

This ensures that embeds are added before encryption, which is required for the feature to work correctly.

### Technical Architecture

#### Core Components

1. **PdfEngine Interface** (`pkg/gotenberg/pdfengine.go`)
   - Defines `EmbedFiles(ctx, logger, filePaths, inputPath)` method

2. **Context & FormData** (`pkg/modules/api/`)
   - `Context.filesByField` tracks files by form field name
   - `FormData.Embeds()` extracts files from "embeds" field

3. **Helper Functions** (`pkg/modules/pdfengines/routes.go`)
   - `FormDataPdfEmbeds()` - extracts embed paths
   - `EmbedFilesStub()` - applies embeds to PDFs

4. **pdfcpu Implementation** (`pkg/modules/pdfcpu/pdfcpu.go`)
   - Uses `pdfcpu attachments add <inputPDF> <files...>` command

#### File Tracking

Files uploaded with the `embeds` field name are tracked separately from main content files:

```go
// Files are organized by field name
filesByField["files"] = ["/path/to/index.html"]
filesByField["embeds"] = ["/path/to/file1.pdf", "/path/to/file2.xml"]
```

## Configuration

### Embed Engines

Configure which PDF engines handle embeds:

```bash
--pdfengines-embed-engines=pdfcpu
```

To disable embeds, use an empty list:

```bash
--pdfengines-embed-engines=
```

## Testing

Run integration tests to verify the feature:

```bash
make test-integration
```

The test suite includes:
- Embedding multiple files into a PDF
- Verifying embeds are added correctly
- Checking PDF content remains unchanged

## Limitations

1. **Engine Support**: Only pdfcpu currently supports embeds
2. **File Size**: Embeds are subject to the same body size limits as other uploaded files
3. **Encryption**: Embeds must be added before PDF encryption

## Examples

### Basic HTML to PDF with Embeds

```bash
curl -X POST http://localhost:3000/forms/chromium/convert/html \
  -F "files=@report.html" \
  -F "embeds=@data.csv" \
  -o report.pdf
```

### Merge PDFs with Embeds

```bash
curl -X POST http://localhost:3000/forms/pdfengines/merge \
  -F "files=@document1.pdf" \
  -F "files=@document2.pdf" \
  -F "embeds=@metadata.json" \
  -o merged.pdf
```

### URL to PDF with Multiple Embeds

```bash
curl -X POST http://localhost:3000/forms/chromium/convert/url \
  -F "url=https://example.com" \
  -F "embeds=@invoice.pdf" \
  -F "embeds=@receipt.pdf" \
  -F "embeds=@contract.pdf" \
  -o webpage.pdf
```

## Extracting Embeds

Embedded files can be extracted using PDF tools like:

- **pdfcpu**: `pdfcpu attachments extract input.pdf outputDir`
- **pdftk**: `pdftk input.pdf unpack_files output outputDir`
- **Adobe Acrobat**: File → Save As → Extract All Files
- **PDF readers**: Most modern PDF readers can view and extract embedded files

## Error Handling

If an unsupported PDF engine is configured for embeds:

```json
{
  "status": 500,
  "message": "embed files into PDF using multi PDF engines: embed files with QPDF: PDF engine method not supported"
}
```

To resolve, ensure pdfcpu is in the embed engines list:

```bash
--pdfengines-embed-engines=pdfcpu
```
