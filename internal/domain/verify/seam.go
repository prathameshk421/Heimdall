package verify

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Import is one import in a test file that breaks the seam rule (TECH_SPEC 12.1, D39): tests reach the app only
// over the declared seam, never by importing an internal package of the system under test.
type Import struct {
	File string
	Line int
	Path string
}

// SeamImports reports the imports of Go test files that match a forbidden prefix, in path order and then source
// order. Each prefix covers that package and everything under it, so "example/internal/app" also forbids
// "example/internal/app/intake"; an empty prefix matches nothing, because that would forbid everything.
//
// Only *_test.go files are checked. A file that cannot be scanned is an error, never a silent pass.
func SeamImports(files map[string]string, forbiddenPrefixes []string) ([]Import, error) {
	var violations []Import
	var problems []error
	for _, path := range slices.Sorted(maps.Keys(files)) {
		if !strings.HasSuffix(path, "_test.go") {
			continue
		}
		specs, err := scanImports(files[path])
		if err != nil {
			problems = append(problems, fmt.Errorf("scan %s: %w", path, err))
			continue
		}
		for _, spec := range specs {
			if forbiddenImport(spec.Path, forbiddenPrefixes) {
				violations = append(violations, Import{File: path, Line: spec.Line, Path: spec.Path})
			}
		}
	}
	if err := errors.Join(problems...); err != nil {
		return nil, err
	}
	return violations, nil
}

func forbiddenImport(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if prefix == "" {
			continue
		}
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

type importSpec struct {
	Path string
	Line int
}

// The scanner reads only import declarations. It is hand-written because the domain lint list does not allow
// go/parser, and it has to skip comments and string literals to avoid reading an "import" that is only text.

type lexKind int

const (
	lexIdent lexKind = iota
	lexString
	lexPunct
	lexEOF
)

type lexeme struct {
	kind lexKind
	text string
	line int
}

type goScanner struct {
	src  string
	pos  int
	line int
}

func (s *goScanner) eof() bool { return s.pos >= len(s.src) }

func (s *goScanner) advance() {
	if s.src[s.pos] == '\n' {
		s.line++
	}
	s.pos++
}

func (s *goScanner) skipSpaceAndComments() error {
	for !s.eof() {
		switch {
		case s.src[s.pos] == ' ' || s.src[s.pos] == '\t' || s.src[s.pos] == '\r' || s.src[s.pos] == '\n':
			s.advance()
		case strings.HasPrefix(s.src[s.pos:], "//"):
			for !s.eof() && s.src[s.pos] != '\n' {
				s.advance()
			}
		case strings.HasPrefix(s.src[s.pos:], "/*"):
			if err := s.skipBlockComment(); err != nil {
				return err
			}
		default:
			return nil
		}
	}
	return nil
}

func (s *goScanner) skipBlockComment() error {
	line := s.line
	s.pos += 2
	end := strings.Index(s.src[s.pos:], "*/")
	if end < 0 {
		return fmt.Errorf("line %d: unterminated block comment", line)
	}
	for _, c := range s.src[s.pos : s.pos+end] {
		if c == '\n' {
			s.line++
		}
	}
	s.pos += end + 2
	return nil
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func (s *goScanner) next() (lexeme, error) {
	if err := s.skipSpaceAndComments(); err != nil {
		return lexeme{}, err
	}
	if s.eof() {
		return lexeme{kind: lexEOF, line: s.line}, nil
	}
	line := s.line
	switch c := s.src[s.pos]; {
	case isIdentByte(c):
		start := s.pos
		for !s.eof() && isIdentByte(s.src[s.pos]) {
			s.advance()
		}
		return lexeme{kind: lexIdent, text: s.src[start:s.pos], line: line}, nil
	case c == '"' || c == '`' || c == '\'':
		literal, err := s.readLiteral(c, line)
		if err != nil {
			return lexeme{}, err
		}
		return lexeme{kind: lexString, text: literal, line: line}, nil
	default:
		s.advance()
		return lexeme{kind: lexPunct, text: string(c), line: line}, nil
	}
}

func (s *goScanner) readLiteral(quote byte, line int) (string, error) {
	start := s.pos
	s.advance()
	for !s.eof() {
		c := s.src[s.pos]
		if c == '\\' && quote != '`' {
			s.advance()
			if s.eof() {
				break
			}
			s.advance()
			continue
		}
		if c == quote {
			s.advance()
			return s.src[start:s.pos], nil
		}
		if c == '\n' && quote != '`' {
			return "", fmt.Errorf("line %d: unterminated literal", line)
		}
		s.advance()
	}
	return "", fmt.Errorf("line %d: unterminated literal", line)
}

func scanImports(src string) ([]importSpec, error) {
	s := &goScanner{src: src, line: 1}
	var out []importSpec
	depth := 0
	for {
		tok, err := s.next()
		if err != nil {
			return nil, err
		}
		switch tok.kind {
		case lexEOF:
			return out, nil
		case lexPunct:
			switch tok.text {
			case "{":
				depth++
			case "}":
				depth--
			}
		case lexIdent:
			if tok.text != "import" || depth != 0 {
				continue
			}
			specs, err := s.readImportDecl()
			if err != nil {
				return nil, err
			}
			out = append(out, specs...)
		case lexString:
		}
	}
}

func (s *goScanner) readImportDecl() ([]importSpec, error) {
	tok, err := s.next()
	if err != nil {
		return nil, err
	}
	if tok.kind == lexPunct && tok.text == "(" {
		return s.readImportGroup()
	}
	spec, err := s.readImportSpec(tok)
	if err != nil {
		return nil, err
	}
	return []importSpec{spec}, nil
}

func (s *goScanner) readImportGroup() ([]importSpec, error) {
	var out []importSpec
	for {
		tok, err := s.next()
		if err != nil {
			return nil, err
		}
		switch {
		case tok.kind == lexEOF:
			return nil, fmt.Errorf("line %d: unterminated import block", tok.line)
		case tok.kind == lexPunct && tok.text == ")":
			return out, nil
		}
		spec, err := s.readImportSpec(tok)
		if err != nil {
			return nil, err
		}
		out = append(out, spec)
	}
}

// readImportSpec accepts an optional name (an identifier, "_" or ".") and then the quoted path.
func (s *goScanner) readImportSpec(first lexeme) (importSpec, error) {
	tok := first
	if tok.kind != lexString {
		if tok.kind != lexIdent && (tok.kind != lexPunct || tok.text != ".") {
			return importSpec{}, fmt.Errorf("line %d: import path expected, got %q", tok.line, tok.text)
		}
		next, err := s.next()
		if err != nil {
			return importSpec{}, err
		}
		tok = next
	}
	if tok.kind != lexString {
		return importSpec{}, fmt.Errorf("line %d: import path expected, got %q", tok.line, tok.text)
	}
	path, err := strconv.Unquote(tok.text)
	if err != nil {
		return importSpec{}, fmt.Errorf("line %d: bad import path %s: %w", tok.line, tok.text, err)
	}
	return importSpec{Path: path, Line: tok.line}, nil
}
