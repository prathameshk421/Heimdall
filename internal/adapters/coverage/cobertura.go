package coverage

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"

	domaincoverage "github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/coverage"
)

// maxDepth bounds element nesting. A Cobertura report is nine levels deep at most (coverage, packages,
// package, classes, class, methods, method, lines, line); the limit leaves room for other generators and
// stops a small input from holding the decoder's element stack open.
const maxDepth = 32

// ParseCobertura reads a Cobertura XML coverage report. It reads the filename of every class and the
// number and hits of every line under it. Reports written by Java Cobertura and by coverage.py parse the
// same way; everything else in the document is skipped.
//
// The size limit is checked before decoding. A malformed document returns domaincoverage.ErrMalformed
// with the source line, and an input over the limit returns domaincoverage.ErrTooLarge, so a caller
// branches with errors.Is. A bad option is a plain error.
func ParseCobertura(data []byte, opt domaincoverage.Options) (domaincoverage.Result, error) {
	b, err := domaincoverage.NewBuilder(opt)
	if err != nil {
		return domaincoverage.Result{}, err
	}
	if err := b.CheckSize(data); err != nil {
		return domaincoverage.Result{}, err
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	r := &coberturaReader{b: b, dec: dec, data: data}
	if err := r.run(); err != nil {
		return domaincoverage.Result{}, err
	}
	return b.Result(), nil
}

// coberturaReader streams the tokens of one document. Token checks that every end tag matches its start
// tag, so the reader only counts depth. classDepth is the depth of the open class, or 0.
type coberturaReader struct {
	b    *domaincoverage.Builder
	dec  *xml.Decoder
	data []byte

	// lineOffset and line cache the line number of lineOffset, so finding the line of each token
	// costs the bytes since the last one, not the bytes since the start.
	lineOffset int64
	line       int

	depth      int
	rootDone   bool
	doctype    bool
	classDepth int
	file       string
}

func (r *coberturaReader) run() error {
	r.line = 1
	for {
		offset := r.dec.InputOffset()
		token, err := r.dec.Token()
		if errors.Is(err, io.EOF) {
			return r.finish(offset)
		}
		if err != nil {
			return r.decodeError(err, offset)
		}
		if err := r.handle(token, r.lineAt(offset)); err != nil {
			return err
		}
	}
}

// handle takes one token that began on line.
func (r *coberturaReader) handle(token xml.Token, line int) error {
	switch t := token.(type) {
	case xml.StartElement:
		return r.start(t, line)
	case xml.EndElement:
		r.end()
	case xml.Directive:
		return r.directive(t, line)
	case xml.CharData:
		if r.depth == 0 && len(bytes.TrimSpace(t)) > 0 {
			return malformed(line, "text outside the coverage element")
		}
	}
	return nil
}

func (r *coberturaReader) finish(offset int64) error {
	if !r.rootDone {
		return malformed(r.lineAt(offset), "no coverage element")
	}
	return nil
}

// decodeError turns a decoder failure into ErrMalformed. A syntax error names its own line; any other
// failure, such as a declared character set the reader does not have, names the line it stopped on.
func (r *coberturaReader) decodeError(err error, offset int64) error {
	var syntax *xml.SyntaxError
	if errors.As(err, &syntax) {
		return malformed(syntax.Line, "%s", syntax.Msg)
	}
	return malformed(r.lineAt(offset), "%v", err)
}

func (r *coberturaReader) start(t xml.StartElement, line int) error {
	if r.depth == 0 {
		if r.rootDone {
			return malformed(line, "element %q after the coverage element", t.Name.Local)
		}
		if t.Name.Local != "coverage" {
			return malformed(line, "root element is %q, want coverage", t.Name.Local)
		}
	}
	r.depth++
	if r.depth > maxDepth {
		return malformed(line, "elements are nested deeper than %d", maxDepth)
	}
	switch t.Name.Local {
	case "class":
		return r.class(t, line)
	case "line":
		return r.lineElement(t, line)
	}
	return nil
}

func (r *coberturaReader) end() {
	if r.depth == r.classDepth {
		r.file, r.classDepth = "", 0
	}
	r.depth--
	if r.depth == 0 {
		r.rootDone = true
	}
}

// directive accepts one plain external document type declaration before the root, which is what Java
// Cobertura writes. coverage.py 4.5.4, 5.5 and 7.17.4 write none (docs/product/research/12). Go's decoder does not read or expand a directive, so nothing in a
// declaration is ever fetched; an internal subset or an ENTITY declaration could still mean the report
// relies on an entity, so those are refused, and so is any other directive.
func (r *coberturaReader) directive(t xml.Directive, line int) error {
	switch {
	case r.depth > 0 || r.rootDone:
		return malformed(line, "declaration inside the document")
	case r.doctype:
		return malformed(line, "second document type declaration")
	case !bytes.HasPrefix(t, []byte("DOCTYPE")):
		return malformed(line, "unexpected declaration")
	case bytes.ContainsAny(t, "[<"):
		return malformed(line, "internal document type subsets are not accepted")
	}
	r.doctype = true
	return nil
}

func (r *coberturaReader) class(t xml.StartElement, line int) error {
	if r.classDepth != 0 {
		return malformed(line, "class inside the class opened before it")
	}
	attrs, err := readAttrs(t, line, "filename")
	if err != nil {
		return err
	}
	raw := attrs[0]
	if raw == "" {
		return malformed(line, "class has no filename")
	}
	if err := r.b.CheckFile(line, raw); err != nil {
		return err
	}
	r.file, r.classDepth = raw, r.depth
	return nil
}

func (r *coberturaReader) lineElement(t xml.StartElement, line int) error {
	if r.file == "" {
		return malformed(line, "line is outside a class")
	}
	attrs, err := readAttrs(t, line, "number", "hits")
	if err != nil {
		return err
	}
	number, err := count(attrs[0], line, "number")
	if err != nil {
		return err
	}
	hits, err := count(attrs[1], line, "hits")
	if err != nil {
		return err
	}
	return r.b.Add(line, r.file, number, hits)
}

// readAttrs returns the values of the named attributes, in order. A missing name or a name that appears
// twice is refused, because the report would otherwise mean two things.
func readAttrs(t xml.StartElement, line int, names ...string) ([]string, error) {
	values := make([]string, len(names))
	seen := make([]bool, len(names))
	for _, a := range t.Attr {
		if a.Name.Space != "" {
			continue
		}
		for i, name := range names {
			if a.Name.Local != name {
				continue
			}
			if seen[i] {
				return nil, malformed(line, "attribute %q appears twice", name)
			}
			seen[i], values[i] = true, a.Value
		}
	}
	for i, name := range names {
		if !seen[i] {
			return nil, malformed(line, "%q element has no %s attribute", t.Name.Local, name)
		}
	}
	return values, nil
}

func count(raw string, line int, what string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, malformed(line, "%s %q is not a number", what, raw)
	}
	return n, nil
}

// lineAt returns the 1-based line of a byte offset. Offsets only grow, so it counts from the last one.
func (r *coberturaReader) lineAt(offset int64) int {
	if offset > int64(len(r.data)) {
		offset = int64(len(r.data))
	}
	if offset > r.lineOffset {
		r.line += bytes.Count(r.data[r.lineOffset:offset], []byte{'\n'})
		r.lineOffset = offset
	}
	return r.line
}

// malformed builds a parse error that names the source line and wraps ErrMalformed.
func malformed(line int, format string, args ...any) error {
	return fmt.Errorf("line %d: %s: %w", line, fmt.Sprintf(format, args...), domaincoverage.ErrMalformed)
}
