package api

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

// DOCX MIME type — application/vnd.openxmlformats-officedocument.wordprocessingml.document
const docxContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// wordMLNS is the WordprocessingML XML namespace used by all w:* elements.
const wordMLNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

// DOCXExtractor extracts Markdown-flavored text from a DOCX (OOXML) archive.
type DOCXExtractor struct {
	limits ooxmlLimits
}

// NewDOCXExtractor returns an extractor configured with the given limits.
func NewDOCXExtractor(limits ooxmlLimits) *DOCXExtractor {
	return &DOCXExtractor{limits: limits}
}

// Name returns the extractor name as registered with the registry.
func (e *DOCXExtractor) Name() string { return "docx" }

// CanHandle returns true iff contentType is the DOCX OOXML MIME type.
func (e *DOCXExtractor) CanHandle(contentType string) bool {
	return strings.EqualFold(contentType, docxContentType)
}

// Bounded marks DOCXExtractor as needing a wall-clock deadline.
func (e *DOCXExtractor) Bounded() bool { return true }

// Extract parses a DOCX archive and produces Markdown-flavored text.
func (e *DOCXExtractor) Extract(data []byte, contentType string) (ExtractedContent, error) {
	opener := newOOXMLOpener(e.limits)
	arch, err := opener.open(data)
	if err != nil {
		return ExtractedContent{}, err
	}
	rdr, err := arch.openMember("word/document.xml")
	if err != nil {
		return ExtractedContent{}, err
	}
	defer func() { _ = rdr.Close() }()

	mb := newMarkdownBuilder(e.limits.MarkdownSizeBytes)
	d := newBoundedXMLDecoder(rdr, e.limits.MaxXMLElementDepth)

	st := &docxState{mb: mb}
	if err := docxRenderBody(d, st); err != nil {
		return ExtractedContent{}, err
	}

	return ExtractedContent{
		Text:        strings.TrimRight(st.mb.String(), "\n"),
		Title:       st.title,
		ContentType: contentType,
	}, nil
}

// docxState carries cross-element rendering state through the streaming pass.
type docxState struct {
	mb    *markdownBuilder
	title string
}

// docxParaState tracks state for a single paragraph being assembled.
type docxParaState struct {
	headingLevel int             // 0 = not a heading, 1-6 = H1-H6
	runText      strings.Builder // accumulated text for this paragraph
}

// docxRenderBody walks the body token stream and writes Markdown.
// Phase A scope: paragraphs, headings (Heading1..Heading6), basic run text.
// Later phases will extend this for tables/lists/hyperlinks/drawings/footnotes.
func docxRenderBody(d *boundedXMLDecoder, st *docxState) error {
	var p *docxParaState
	first := true

	emitPara := func() error {
		if p == nil {
			return nil
		}
		text := strings.TrimSpace(p.runText.String())
		if text == "" {
			p = nil
			return nil
		}
		if !first {
			if _, err := st.mb.WriteString("\n\n"); err != nil {
				return err
			}
		}
		first = false
		var prefix string
		if p.headingLevel > 0 {
			prefix = strings.Repeat("#", p.headingLevel) + " "
			if st.title == "" && p.headingLevel == 1 {
				st.title = text
			}
		}
		if _, err := st.mb.WriteString(prefix + text); err != nil {
			return err
		}
		p = nil
		return nil
	}

	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wordMLNS {
				continue
			}
			switch t.Name.Local {
			case "p":
				p = &docxParaState{}
			case "pStyle":
				if p != nil {
					for _, a := range t.Attr {
						if a.Name.Local == "val" && strings.HasPrefix(a.Value, "Heading") {
							levelByte := a.Value[len("Heading"):]
							if len(levelByte) == 1 && levelByte[0] >= '1' && levelByte[0] <= '6' {
								p.headingLevel = int(levelByte[0] - '0')
							}
						}
					}
				}
			case "t":
				// Read the text node directly via DecodeElement to capture chardata.
				var text string
				if err := d.DecodeElement(&text, &t); err != nil {
					return err
				}
				if p != nil {
					p.runText.WriteString(text)
				}
			}
		case xml.EndElement:
			if t.Name.Space == wordMLNS && t.Name.Local == "p" {
				if err := emitPara(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
