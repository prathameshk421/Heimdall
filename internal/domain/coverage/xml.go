package coverage

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf8"
)

// This file holds a small, strict XML reader for Cobertura reports. It is hand-written on purpose: the
// domain import list does not allow encoding/xml, and an untrusted coverage artifact gets a reader with
// no external entity fetch, no internal document type subset and no entity expansion beyond the five XML
// entities and numeric references.

type xmlKind int

const (
	xmlStart xmlKind = iota
	xmlEnd
	xmlSelfClose
	xmlEOF
)

// xmlToken is one element tag. Text is skipped because Cobertura keeps every value this package reads
// in an attribute.
type xmlToken struct {
	kind  xmlKind
	name  string
	attrs map[string]string
	line  int
}

// attr returns an attribute value, or "" when the attribute is absent.
func (t xmlToken) attr(name string) string { return t.attrs[name] }

// xmlScanner reads element tags from data and tracks the source line of each one.
type xmlScanner struct {
	data []byte
	pos  int
	line int
}

func newXMLScanner(data []byte) *xmlScanner { return &xmlScanner{data: data, line: 1} }

func (s *xmlScanner) eof() bool { return s.pos >= len(s.data) }

func (s *xmlScanner) startsWith(prefix string) bool {
	return bytes.HasPrefix(s.data[s.pos:], []byte(prefix))
}

func (s *xmlScanner) advance() {
	if s.data[s.pos] == '\n' {
		s.line++
	}
	s.pos++
}

func (s *xmlScanner) skipSpace() {
	for !s.eof() {
		switch s.data[s.pos] {
		case ' ', '\t', '\r', '\n':
			s.advance()
		default:
			return
		}
	}
}

// next returns the next element tag, or xmlEOF.
func (s *xmlScanner) next() (xmlToken, error) {
	if err := s.skipMarkup(); err != nil {
		return xmlToken{}, err
	}
	if s.eof() {
		return xmlToken{kind: xmlEOF, line: s.line}, nil
	}
	return s.readTag()
}

// skipMarkup steps over text, comments, processing instructions, CDATA and the document type
// declaration that a Cobertura report starts with.
func (s *xmlScanner) skipMarkup() error {
	for !s.eof() {
		if s.data[s.pos] != '<' {
			for !s.eof() && s.data[s.pos] != '<' {
				s.advance()
			}
			continue
		}
		skipped, err := s.skipProlog()
		if err != nil {
			return err
		}
		if !skipped {
			return nil
		}
	}
	return nil
}

// skipProlog consumes one comment, processing instruction, CDATA section or document type declaration.
// It reports whether it consumed one, so the caller knows whether a real tag starts at pos.
func (s *xmlScanner) skipProlog() (bool, error) {
	switch {
	case s.startsWith("<!--"):
		return true, s.skipUntil("<!--", "-->", "comment")
	case s.startsWith("<?"):
		return true, s.skipUntil("<?", "?>", "processing instruction")
	case s.startsWith("<![CDATA["):
		return true, s.skipUntil("<![CDATA[", "]]>", "CDATA section")
	case s.startsWith("<!DOCTYPE"):
		return true, s.skipDoctype()
	case s.startsWith("<!"):
		return false, malformed(s.line, "unexpected declaration")
	default:
		return false, nil
	}
}

func (s *xmlScanner) skipUntil(prefix, suffix, what string) error {
	line := s.line
	s.pos += len(prefix)
	for !s.eof() {
		if s.startsWith(suffix) {
			s.pos += len(suffix)
			return nil
		}
		s.advance()
	}
	return malformed(line, "unterminated %s", what)
}

// skipDoctype steps over an external document type declaration. An internal subset could declare
// entities, so it is refused rather than expanded.
func (s *xmlScanner) skipDoctype() error {
	line := s.line
	s.pos += len("<!DOCTYPE")
	for !s.eof() {
		switch s.data[s.pos] {
		case '>':
			s.pos++
			return nil
		case '[':
			return malformed(s.line, "internal document type subsets are not accepted")
		default:
			s.advance()
		}
	}
	return malformed(line, "unterminated document type declaration")
}

func (s *xmlScanner) readTag() (xmlToken, error) {
	line := s.line
	s.pos++
	if s.startsWith("/") {
		s.pos++
		name, err := s.readName()
		if err != nil {
			return xmlToken{}, err
		}
		s.skipSpace()
		if err := s.expect('>', line); err != nil {
			return xmlToken{}, err
		}
		return xmlToken{kind: xmlEnd, name: name, line: line}, nil
	}
	name, err := s.readName()
	if err != nil {
		return xmlToken{}, err
	}
	attrs := map[string]string{}
	for {
		s.skipSpace()
		if s.eof() {
			return xmlToken{}, malformed(line, "element %q is not closed", name)
		}
		if s.data[s.pos] == '>' {
			s.pos++
			return xmlToken{kind: xmlStart, name: name, attrs: attrs, line: line}, nil
		}
		if s.data[s.pos] == '/' {
			s.pos++
			if err := s.expect('>', line); err != nil {
				return xmlToken{}, err
			}
			return xmlToken{kind: xmlSelfClose, name: name, attrs: attrs, line: line}, nil
		}
		if err := s.readAttribute(attrs, line); err != nil {
			return xmlToken{}, err
		}
	}
}

func (s *xmlScanner) readAttribute(attrs map[string]string, tagLine int) error {
	name, err := s.readName()
	if err != nil {
		return err
	}
	s.skipSpace()
	if err := s.expect('=', tagLine); err != nil {
		return err
	}
	s.skipSpace()
	value, err := s.readValue(tagLine)
	if err != nil {
		return err
	}
	if _, duplicate := attrs[name]; duplicate {
		return malformed(tagLine, "attribute %q appears twice", name)
	}
	attrs[name] = value
	return nil
}

func (s *xmlScanner) readValue(tagLine int) (string, error) {
	if s.eof() {
		return "", malformed(tagLine, "attribute value expected")
	}
	quote := s.data[s.pos]
	if quote != '"' && quote != '\'' {
		return "", malformed(s.line, "attribute value must be quoted")
	}
	s.pos++
	start := s.pos
	for !s.eof() && s.data[s.pos] != quote {
		if s.data[s.pos] == '<' {
			return "", malformed(s.line, "attribute value contains an unescaped <")
		}
		s.advance()
	}
	if s.eof() {
		return "", malformed(tagLine, "attribute value is not closed")
	}
	raw := s.data[start:s.pos]
	s.pos++
	return decodeXMLText(raw, tagLine)
}

func (s *xmlScanner) readName() (string, error) {
	start := s.pos
	for !s.eof() && nameByte(s.data[s.pos]) {
		s.pos++
	}
	if s.pos == start {
		return "", malformed(s.line, "element or attribute name expected")
	}
	return string(s.data[start:s.pos]), nil
}

func (s *xmlScanner) expect(want byte, tagLine int) error {
	if s.eof() {
		return malformed(tagLine, "%q expected, the report ends", string(want))
	}
	if s.data[s.pos] != want {
		return malformed(s.line, "%q expected, got %q", string(want), string(s.data[s.pos]))
	}
	s.pos++
	return nil
}

func nameByte(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '/', '>', '<', '=', '"', '\'', '&':
		return false
	default:
		return true
	}
}

// decodeXMLText expands the five XML entities and numeric character references. Any other reference is
// refused, because a report has no reason to carry one and the reader expands no custom entity.
func decodeXMLText(raw []byte, tagLine int) (string, error) {
	if !bytes.ContainsRune(raw, '&') {
		return string(raw), nil
	}
	var out strings.Builder
	for i := 0; i < len(raw); {
		if raw[i] != '&' {
			out.WriteByte(raw[i])
			i++
			continue
		}
		end := bytes.IndexByte(raw[i:], ';')
		if end < 0 {
			return "", malformed(tagLine, "unterminated entity reference")
		}
		entity := string(raw[i+1 : i+end])
		if err := writeEntity(&out, entity, tagLine); err != nil {
			return "", err
		}
		i += end + 1
	}
	return out.String(), nil
}

func writeEntity(out *strings.Builder, entity string, tagLine int) error {
	switch entity {
	case "amp":
		out.WriteByte('&')
	case "lt":
		out.WriteByte('<')
	case "gt":
		out.WriteByte('>')
	case "quot":
		out.WriteByte('"')
	case "apos":
		out.WriteByte('\'')
	default:
		r, ok := numericEntity(entity)
		if !ok {
			return malformed(tagLine, "unknown entity %q", entity)
		}
		out.WriteRune(r)
	}
	return nil
}

func numericEntity(entity string) (rune, bool) {
	if !strings.HasPrefix(entity, "#") {
		return 0, false
	}
	digits, base := entity[1:], 10
	if len(digits) > 1 && (digits[0] == 'x' || digits[0] == 'X') {
		digits, base = digits[1:], 16
	}
	n, err := strconv.ParseInt(digits, base, 32)
	if err != nil {
		return 0, false
	}
	r := rune(n)
	if !utf8.ValidRune(r) {
		return 0, false
	}
	return r, true
}
