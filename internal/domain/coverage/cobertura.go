package coverage

import "strconv"

// ParseCobertura reads a Cobertura XML coverage report. It reads the filename of every class and the
// number and hits of every line under it. Reports written by Java Cobertura and by coverage.py parse
// the same way; everything else in the document is skipped.
func ParseCobertura(data []byte, opt Options) (Result, error) {
	prefix, err := checkSize(data, opt)
	if err != nil {
		return Result{}, err
	}
	st := newState(prefix)
	reader := &coberturaReader{st: st}
	if err := reader.run(newXMLScanner(data)); err != nil {
		return Result{}, err
	}
	return Result{Files: st.files}, nil
}

// coberturaReader walks the elements and keeps the open class. stack holds the open element names, so
// an unclosed or mismatched element is refused.
type coberturaReader struct {
	st    *parseState
	stack []string
	file  string
	root  bool
}

func (r *coberturaReader) run(s *xmlScanner) error {
	for {
		token, err := s.next()
		if err != nil {
			return err
		}
		if token.kind == xmlEOF {
			return r.finish(token.line)
		}
		if err := r.token(token); err != nil {
			return err
		}
	}
}

func (r *coberturaReader) finish(line int) error {
	if !r.root {
		return malformed(line, "no coverage element")
	}
	if len(r.stack) > 0 {
		return malformed(line, "element %q is not closed", r.stack[len(r.stack)-1])
	}
	return nil
}

func (r *coberturaReader) token(token xmlToken) error {
	if !r.root {
		if token.kind == xmlEnd || token.name != "coverage" {
			return malformed(token.line, "root element is %q, want coverage", token.name)
		}
		r.root = true
	}
	if token.kind == xmlEnd {
		return r.close(token)
	}
	if err := r.open(token); err != nil {
		return err
	}
	if token.kind == xmlStart {
		r.stack = append(r.stack, token.name)
		return nil
	}
	if token.name == "class" {
		r.file = ""
	}
	return nil
}

func (r *coberturaReader) open(token xmlToken) error {
	switch token.name {
	case "class":
		if r.file != "" {
			return malformed(token.line, "class inside the class opened before it")
		}
		raw := token.attr("filename")
		if raw == "" {
			return malformed(token.line, "class has no filename")
		}
		file, err := r.st.file(raw, token.line)
		if err != nil {
			return err
		}
		r.file = file
	case "line":
		return r.line(token)
	}
	return nil
}

func (r *coberturaReader) line(token xmlToken) error {
	if r.file == "" {
		return malformed(token.line, "line is outside a class")
	}
	number, err := attrCount(token, "number", 1)
	if err != nil {
		return err
	}
	hits, err := attrCount(token, "hits", 0)
	if err != nil {
		return err
	}
	return r.st.add(token.line, r.file, number, hits)
}

func (r *coberturaReader) close(token xmlToken) error {
	if len(r.stack) == 0 {
		return malformed(token.line, "closing tag %q has no open element", token.name)
	}
	if top := r.stack[len(r.stack)-1]; top != token.name {
		return malformed(token.line, "closing tag %q does not match %q", token.name, top)
	}
	r.stack = r.stack[:len(r.stack)-1]
	if token.name == "class" {
		r.file = ""
	}
	return nil
}

func attrCount(token xmlToken, name string, smallest int) (int, error) {
	raw, ok := token.attrs[name]
	if !ok {
		return 0, malformed(token.line, "%q element has no %s attribute", token.name, name)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, malformed(token.line, "%s %q is not a number", name, raw)
	}
	if n < smallest {
		return 0, malformed(token.line, "%s %d is below %d", name, n, smallest)
	}
	return n, nil
}
