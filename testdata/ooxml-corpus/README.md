# OOXML Real-Corpus Test Fixtures

This directory holds real-document fixtures for build-tagged regression tests
in `api/ooxml_extractors_corpus_test.go`. Each `<name>.docx` / `.pptx` / `.xlsx`
must have a sibling `<name>.expected.md` that captures the extractor's current
output exactly (byte-for-byte). The corpus test runs each file through the
matching extractor and fails on any diff.

## Running

```bash
make test-corpus-ooxml
```

## Adding a fixture

1. Place the small real document (< 50 KB ideally) in this directory.
2. Run the extractor to capture current output:

   ```bash
   go test -tags=corpus ./api -run TestOOXMLCorpus/<filename> -v 2>&1 | tee out.txt
   ```

   On first run with no `.expected.md`, the test will fail with
   `missing .expected.md sibling`. Inspect the actual extracted output,
   save it to `<name>.expected.md`, and re-run.

3. Commit both the source fixture and `.expected.md` together.

## Guidelines

- Keep fixtures small (< 50 KB each).
- **Do not commit any document containing PII, proprietary content, or copyrighted material.** Synthetic content only.
- One fixture per "interesting shape" per format: e.g., docx with tables, pptx
  with multiple slides + speaker notes, xlsx with merged cells and dates.
- If extractor behavior intentionally changes, regenerate `.expected.md` files
  in the same commit.

## Generating synthetic fixtures

For DOCX:
```bash
echo -e "# Test Title\n\nHello world." > sample.md
soffice --headless --convert-to docx sample.md
```

For PPTX/XLSX, use LibreOffice Impress/Calc to create small files manually,
or generate programmatically via excelize for XLSX.
