package verify

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Status is the outcome of one test case.
type Status string

// The four statuses a case can have. A case with a <failure> fails; a <error> is a compile or setup problem, and a
// skipped test proves nothing (TECH_SPEC 12.1).
const (
	StatusPass    Status = "pass"
	StatusFail    Status = "fail"
	StatusError   Status = "error"
	StatusSkipped Status = "skipped"
)

// Valid reports whether s is one of the four statuses.
func (s Status) Valid() bool {
	switch s {
	case StatusPass, StatusFail, StatusError, StatusSkipped:
		return true
	}
	return false
}

// Case is one test case from a JUnit report. Suite and ClassName are kept as the tools wrote them: gotestsum puts
// the package in both, pytest puts the module in ClassName.
type Case struct {
	Suite     string
	ClassName string
	Name      string
	Status    Status
	// Message is the failure, error or skip message, at most MaxMessageBytes.
	Message string
	// Text is the failure or error body, at most MaxMessageBytes.
	Text string
}

// Suite is one testsuite element.
type Suite struct {
	Name  string
	Cases []Case
}

// Report is one parsed JUnit run.
type Report struct {
	Suites []Suite
}

// Cases returns every case in the report, in document order.
func (r Report) Cases() []Case {
	var out []Case
	for _, s := range r.Suites {
		out = append(out, s.Cases...)
	}
	return out
}

// Limits of a parsed report. No test case is never green, and a message is bounded so a runaway log cannot flood an
// agent or a chat message.
const (
	MaxReportBytes  = 8 << 20
	MaxMessageBytes = 2048
)

// Errors Parse refuses with. A caller branches on them, so they are sentinels.
var (
	ErrTooLarge  = errors.New("junit report is too large")
	ErrMalformed = errors.New("malformed junit report")
	ErrNoCases   = errors.New("junit report has no test cases")
)

// Parse reads one JUnit report from r. At most MaxReportBytes are read, so an endless reader is cut off.
func Parse(r io.Reader) (Report, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxReportBytes+1))
	if err != nil {
		return Report{}, fmt.Errorf("read junit report: %w", err)
	}
	if len(b) > MaxReportBytes {
		return Report{}, fmt.Errorf("junit report is over %d bytes: %w", MaxReportBytes, ErrTooLarge)
	}
	return ParseBytes(b)
}

// ParseBytes reads one JUnit report from data, as written by `gotestsum --junitfile`, by a `go test -json`
// converter, or by pytest.
func ParseBytes(data []byte) (Report, error) {
	if len(data) > MaxReportBytes {
		return Report{}, fmt.Errorf("junit report is %d bytes, at most %d: %w", len(data), MaxReportBytes, ErrTooLarge)
	}
	p := &parser{data: data, line: 1}
	rep, err := p.parseReport()
	if err != nil {
		return Report{}, err
	}
	if len(rep.Cases()) == 0 {
		return Report{}, fmt.Errorf("the run reported no test cases, so it is not green: %w", ErrNoCases)
	}
	return rep, nil
}

// The parser is hand-written on purpose: the domain lint list does not allow encoding/xml, and untrusted sandbox
// output gets a stricter reader anyway. It accepts no document type declaration and expands no custom entity, so
// there is nothing for an entity or a nested document to blow up.

type tokenKind int

const (
	tokStart tokenKind = iota
	tokEnd
	tokSelfClose
	tokText
	tokEOF
)

type attribute struct {
	name  string
	value string
}

type token struct {
	kind  tokenKind
	name  string
	attrs []attribute
	text  string
	line  int
}

func (t token) attr(name string) string {
	for _, a := range t.attrs {
		if a.name == name {
			return a.value
		}
	}
	return ""
}

type parser struct {
	data []byte
	pos  int
	line int
}

func (p *parser) eof() bool { return p.pos >= len(p.data) }

func (p *parser) peek() byte { return p.data[p.pos] }

func (p *parser) startsWith(prefix string) bool {
	return bytes.HasPrefix(p.data[p.pos:], []byte(prefix))
}

func (p *parser) advance() {
	if p.data[p.pos] == '\n' {
		p.line++
	}
	p.pos++
}

func (p *parser) errAt(line int, format string, args ...any) error {
	return fmt.Errorf("line %d: %s: %w", line, fmt.Sprintf(format, args...), ErrMalformed)
}

func (p *parser) errf(format string, args ...any) error { return p.errAt(p.line, format, args...) }

func (p *parser) skipSpace() {
	for !p.eof() {
		switch p.peek() {
		case ' ', '\t', '\r', '\n':
			p.advance()
		default:
			return
		}
	}
}

func (p *parser) skipUntil(prefix, suffix, what string) error {
	line := p.line
	p.pos += len(prefix)
	for !p.eof() {
		if p.startsWith(suffix) {
			p.pos += len(suffix)
			return nil
		}
		p.advance()
	}
	return p.errAt(line, "unterminated %s", what)
}

func (p *parser) expect(c byte) error {
	if p.eof() {
		return p.errf("%q expected, the report ends", string(c))
	}
	if p.peek() != c {
		return p.errf("%q expected, got %q", string(c), string(p.peek()))
	}
	p.advance()
	return nil
}

func isNameByte(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '/', '>', '<', '=', '"', '\'', '&':
		return false
	}
	return true
}

func (p *parser) readName() (string, error) {
	start := p.pos
	for !p.eof() && isNameByte(p.peek()) {
		p.advance()
	}
	if p.pos == start {
		return "", p.errf("element or attribute name expected")
	}
	return string(p.data[start:p.pos]), nil
}

// skipProlog walks the XML declaration, comments and whitespace before the root element.
func (p *parser) skipProlog() error {
	for {
		p.skipSpace()
		switch {
		case p.startsWith("<!--"):
			if err := p.skipUntil("<!--", "-->", "comment"); err != nil {
				return err
			}
		case p.startsWith("<?"):
			if err := p.skipUntil("<?", "?>", "processing instruction"); err != nil {
				return err
			}
		case p.startsWith("<!"):
			return p.errf("document type declarations are not accepted")
		default:
			return nil
		}
	}
}

func (p *parser) readCDATA() (token, error) {
	line := p.line
	p.pos += len("<![CDATA[")
	end := bytes.Index(p.data[p.pos:], []byte("]]>"))
	if end < 0 {
		return token{}, p.errAt(line, "unterminated CDATA section")
	}
	raw := p.data[p.pos : p.pos+end]
	for _, c := range raw {
		if c == '\n' {
			p.line++
		}
	}
	p.pos += end + len("]]>")
	return token{kind: tokText, text: string(raw), line: line}, nil
}

func (p *parser) next() (token, error) {
	for {
		switch {
		case p.eof():
			return token{kind: tokEOF, line: p.line}, nil
		case p.startsWith("<!--"):
			if err := p.skipUntil("<!--", "-->", "comment"); err != nil {
				return token{}, err
			}
		case p.startsWith("<?"):
			if err := p.skipUntil("<?", "?>", "processing instruction"); err != nil {
				return token{}, err
			}
		case p.startsWith("<![CDATA["):
			return p.readCDATA()
		case p.startsWith("<!"):
			return token{}, p.errf("document type declarations are not accepted")
		case p.peek() == '<':
			return p.readTag()
		default:
			return p.readText()
		}
	}
}

func (p *parser) readText() (token, error) {
	line := p.line
	start := p.pos
	for !p.eof() && p.peek() != '<' {
		p.advance()
	}
	text, err := decodeEntities(p.data[start:p.pos])
	if err != nil {
		return token{}, p.errAt(line, "%v", err)
	}
	return token{kind: tokText, text: text, line: line}, nil
}

func (p *parser) readTag() (token, error) {
	line := p.line
	p.advance()
	if p.startsWith("/") {
		p.advance()
		name, err := p.readName()
		if err != nil {
			return token{}, err
		}
		p.skipSpace()
		if err := p.expect('>'); err != nil {
			return token{}, err
		}
		return token{kind: tokEnd, name: name, line: line}, nil
	}
	name, err := p.readName()
	if err != nil {
		return token{}, err
	}
	attrs, kind, err := p.readAttributes()
	if err != nil {
		return token{}, err
	}
	return token{kind: kind, name: name, attrs: attrs, line: line}, nil
}

func (p *parser) readAttributes() ([]attribute, tokenKind, error) {
	var attrs []attribute
	for {
		p.skipSpace()
		if p.eof() {
			return nil, 0, p.errf("the report ends inside a tag")
		}
		switch p.peek() {
		case '>':
			p.advance()
			return attrs, tokStart, nil
		case '/':
			p.advance()
			if err := p.expect('>'); err != nil {
				return nil, 0, err
			}
			return attrs, tokSelfClose, nil
		}
		line := p.line
		name, err := p.readName()
		if err != nil {
			return nil, 0, err
		}
		p.skipSpace()
		if err := p.expect('='); err != nil {
			return nil, 0, err
		}
		p.skipSpace()
		value, err := p.readQuoted(line)
		if err != nil {
			return nil, 0, err
		}
		if hasAttribute(attrs, name) {
			return nil, 0, p.errAt(line, "attribute %q is given twice", name)
		}
		attrs = append(attrs, attribute{name: name, value: value})
	}
}

func hasAttribute(attrs []attribute, name string) bool {
	for _, a := range attrs {
		if a.name == name {
			return true
		}
	}
	return false
}

func (p *parser) readQuoted(line int) (string, error) {
	if p.eof() {
		return "", p.errAt(line, "attribute value expected, the report ends")
	}
	quote := p.peek()
	if quote != '"' && quote != '\'' {
		return "", p.errAt(line, "attribute value must be quoted, got %q", string(quote))
	}
	p.advance()
	start := p.pos
	for !p.eof() && p.peek() != quote {
		if p.peek() == '<' {
			return "", p.errAt(line, "'<' is not allowed in an attribute value")
		}
		p.advance()
	}
	if p.eof() {
		return "", p.errAt(line, "unterminated attribute value")
	}
	value, err := decodeEntities(p.data[start:p.pos])
	if err != nil {
		return "", p.errAt(line, "%v", err)
	}
	p.advance()
	return value, nil
}

// decodeEntities expands the five predefined entities and numeric character references. Any other entity is
// refused: the report is untrusted, and an entity it defines is not worth the risk.
func decodeEntities(raw []byte) (string, error) {
	if bytes.IndexByte(raw, '&') < 0 {
		return string(raw), nil
	}
	var b strings.Builder
	for i := 0; i < len(raw); {
		if raw[i] != '&' {
			b.WriteByte(raw[i])
			i++
			continue
		}
		end := bytes.IndexByte(raw[i:], ';')
		if end < 0 {
			return "", errors.New("unterminated entity reference")
		}
		entity := string(raw[i+1 : i+end])
		r, ok := entityRune(entity)
		if !ok {
			return "", fmt.Errorf("unknown entity &%s;", entity)
		}
		b.WriteRune(r)
		i += end + 1
	}
	return b.String(), nil
}

func entityRune(entity string) (rune, bool) {
	switch entity {
	case "lt":
		return '<', true
	case "gt":
		return '>', true
	case "amp":
		return '&', true
	case "quot":
		return '"', true
	case "apos":
		return '\'', true
	}
	return numericEntity(entity)
}

// numericEntity reads &#65; or &#x42;.
func numericEntity(entity string) (rune, bool) {
	if len(entity) < 2 || entity[0] != '#' {
		return 0, false
	}
	digits, base := entity[1:], 10
	if digits[0] == 'x' || digits[0] == 'X' {
		digits, base = digits[1:], 16
	}
	if digits == "" || len(digits) > 8 {
		return 0, false
	}
	var value int64
	for _, c := range []byte(digits) {
		d, ok := digitValue(c, base)
		if !ok {
			return 0, false
		}
		value = value*int64(base) + int64(d)
	}
	r := rune(value)
	if !utf8.ValidRune(r) {
		return 0, false
	}
	return r, true
}

func digitValue(c byte, base int) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case base == 16 && c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case base == 16 && c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}

func (p *parser) parseReport() (Report, error) {
	if err := p.skipProlog(); err != nil {
		return Report{}, err
	}
	tok, err := p.next()
	if err != nil {
		return Report{}, err
	}
	if tok.kind != tokStart && tok.kind != tokSelfClose {
		return Report{}, p.errf("the report must start with a testsuites or testsuite element")
	}
	switch tok.name {
	case "testsuites":
		if tok.kind == tokSelfClose {
			return Report{}, nil
		}
		return p.parseTestsuites(tok)
	case "testsuite":
		s, err := p.parseSuite(tok)
		if err != nil {
			return Report{}, err
		}
		return Report{Suites: []Suite{s}}, nil
	}
	return Report{}, p.errAt(tok.line, "root element %q is not testsuites or testsuite", tok.name)
}

func (p *parser) parseTestsuites(root token) (Report, error) {
	var rep Report
	for {
		tok, err := p.next()
		if err != nil {
			return Report{}, err
		}
		switch tok.kind {
		case tokEOF:
			return Report{}, p.errAt(root.line, "testsuites is not closed")
		case tokEnd:
			if tok.name != "testsuites" {
				return Report{}, p.errAt(tok.line, "unexpected closing tag %q", tok.name)
			}
			return rep, nil
		case tokStart, tokSelfClose:
			if tok.name != "testsuite" {
				if err := p.skipElement(tok); err != nil {
					return Report{}, err
				}
				continue
			}
			s, err := p.parseSuite(tok)
			if err != nil {
				return Report{}, err
			}
			rep.Suites = append(rep.Suites, s)
		case tokText:
			if strings.TrimSpace(tok.text) != "" {
				return Report{}, p.errAt(tok.line, "unexpected text %q", strings.TrimSpace(tok.text))
			}
		}
	}
}

func (p *parser) parseSuite(start token) (Suite, error) {
	suite := Suite{Name: start.attr("name")}
	if start.kind == tokSelfClose {
		return suite, nil
	}
	for {
		tok, err := p.next()
		if err != nil {
			return Suite{}, err
		}
		done, err := p.applySuiteToken(tok, start, &suite)
		if err != nil {
			return Suite{}, err
		}
		if done {
			return suite, nil
		}
	}
}

func (p *parser) applySuiteToken(tok, start token, suite *Suite) (bool, error) {
	switch tok.kind {
	case tokEOF:
		return false, p.errAt(start.line, "testsuite is not closed")
	case tokEnd:
		if tok.name != "testsuite" {
			return false, p.errAt(tok.line, "unexpected closing tag %q", tok.name)
		}
		return true, nil
	case tokStart, tokSelfClose:
		return false, p.suiteChild(tok, suite)
	case tokText:
		if strings.TrimSpace(tok.text) != "" {
			return false, p.errAt(tok.line, "unexpected text %q", strings.TrimSpace(tok.text))
		}
	}
	return false, nil
}

func (p *parser) suiteChild(tok token, suite *Suite) error {
	switch tok.name {
	case "testcase":
		c, err := p.parseCase(tok, suite.Name)
		if err != nil {
			return err
		}
		suite.Cases = append(suite.Cases, c)
		return nil
	case "testsuite":
		return p.errAt(tok.line, "a nested testsuite is not accepted")
	}
	return p.skipElement(tok)
}

func (p *parser) parseCase(start token, suite string) (Case, error) {
	name := start.attr("name")
	if name == "" {
		return Case{}, p.errAt(start.line, "a testcase needs a name")
	}
	c := Case{Suite: suite, ClassName: start.attr("classname"), Name: name, Status: StatusPass}
	if start.kind == tokSelfClose {
		return c, nil
	}
	for {
		tok, err := p.next()
		if err != nil {
			return Case{}, err
		}
		done, err := p.applyCaseToken(tok, start, name, &c)
		if err != nil {
			return Case{}, err
		}
		if done {
			return c, nil
		}
	}
}

func (p *parser) applyCaseToken(tok, start token, name string, c *Case) (bool, error) {
	switch tok.kind {
	case tokEOF:
		return false, p.errAt(start.line, "testcase %q is not closed", name)
	case tokEnd:
		if tok.name != "testcase" {
			return false, p.errAt(tok.line, "unexpected closing tag %q", tok.name)
		}
		return true, nil
	case tokStart, tokSelfClose:
		return false, p.caseChild(tok, c)
	case tokText:
		if strings.TrimSpace(tok.text) != "" {
			return false, p.errAt(tok.line, "unexpected text %q in testcase %q", strings.TrimSpace(tok.text), name)
		}
	}
	return false, nil
}

func (p *parser) caseChild(tok token, c *Case) error {
	status, ok := outcomeStatus(tok.name)
	if !ok {
		return p.skipElement(tok)
	}
	message, text, err := p.parseOutcome(tok)
	if err != nil {
		return err
	}
	if rank(status) > rank(c.Status) {
		c.Status, c.Message, c.Text = status, message, text
	}
	return nil
}

func outcomeStatus(name string) (Status, bool) {
	switch name {
	case "failure":
		return StatusFail, true
	case "error":
		return StatusError, true
	case "skipped":
		return StatusSkipped, true
	}
	return "", false
}

// rank orders the outcomes so that an <error> outranks a <failure>, which outranks <skipped>.
func rank(s Status) int {
	switch s {
	case StatusPass:
		return 0
	case StatusSkipped:
		return 1
	case StatusFail:
		return 2
	case StatusError:
		return 3
	}
	return 0
}

func (p *parser) parseOutcome(start token) (string, string, error) {
	if start.kind == tokSelfClose {
		return truncateMessage(start.attr("message")), "", nil
	}
	var body strings.Builder
	for {
		tok, err := p.next()
		if err != nil {
			return "", "", err
		}
		switch tok.kind {
		case tokEOF:
			return "", "", p.errAt(start.line, "%s is not closed", start.name)
		case tokEnd:
			if tok.name != start.name {
				return "", "", p.errAt(tok.line, "unexpected closing tag %q", tok.name)
			}
			return truncateMessage(start.attr("message")), truncateMessage(body.String()), nil
		case tokText:
			body.WriteString(tok.text)
		case tokStart, tokSelfClose:
			if err := p.skipElement(tok); err != nil {
				return "", "", err
			}
		}
	}
}

func (p *parser) skipElement(start token) error {
	if start.kind == tokSelfClose {
		return nil
	}
	depth := 1
	for {
		tok, err := p.next()
		if err != nil {
			return err
		}
		switch tok.kind {
		case tokEOF:
			return p.errAt(start.line, "element %q is not closed", start.name)
		case tokStart:
			depth++
		case tokEnd:
			depth--
			if depth == 0 {
				return nil
			}
		case tokSelfClose, tokText:
		}
	}
}

// truncateMessage keeps at most MaxMessageBytes bytes and never splits a rune at the cut.
func truncateMessage(s string) string {
	if len(s) <= MaxMessageBytes {
		return s
	}
	s = s[:MaxMessageBytes]
	for i := 0; i < utf8.UTFMax && len(s) > 0; i++ {
		r, size := utf8.DecodeLastRuneInString(s)
		if r != utf8.RuneError || size > 1 {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}
