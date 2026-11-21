package coverage

import "strings"

// ParseLCOV reads an LCOV tracefile: TN, SF, DA, LF, LH and end_of_record records, plus the
// informational records a real generator writes. One record may appear more than once for a file,
// as a merged report does; the counts add.
func ParseLCOV(data []byte, opt Options) (Result, error) {
	b, err := NewBuilder(opt)
	if err != nil {
		return Result{}, err
	}
	if err := b.CheckSize(data); err != nil {
		return Result{}, err
	}
	reader := &lcovReader{b: b}
	if err := reader.run(splitLines(data)); err != nil {
		return Result{}, err
	}
	return b.Result(), nil
}

// lcovReader walks one tracefile. file and opened describe the SF record that is open.
type lcovReader struct {
	b      *Builder
	file   string
	opened int
}

func (l *lcovReader) run(records []string) error {
	for i, record := range records {
		if record == "" {
			continue
		}
		if err := l.record(i+1, record); err != nil {
			return err
		}
	}
	if l.file != "" {
		return malformed(l.opened, "file record %q is not closed with end_of_record", l.file)
	}
	return nil
}

func (l *lcovReader) record(srcLine int, record string) error {
	if record == "end_of_record" {
		if l.file == "" {
			return malformed(srcLine, "end_of_record with no open file record")
		}
		l.file, l.opened = "", 0
		return nil
	}
	name, value, ok := strings.Cut(record, ":")
	if !ok {
		return malformed(srcLine, "record %q has no value", record)
	}
	if name == "SF" {
		return l.start(srcLine, value)
	}
	if name == "DA" {
		return l.da(srcLine, value)
	}
	if !informationalLCOV(name) {
		return malformed(srcLine, "unknown record %q", name)
	}
	return nil
}

func (l *lcovReader) start(srcLine int, raw string) error {
	if l.file != "" {
		return malformed(srcLine, "SF before end_of_record, the record opened on line %d is still open", l.opened)
	}
	file, err := l.b.file(raw, srcLine)
	if err != nil {
		return err
	}
	l.file, l.opened = file, srcLine
	return nil
}

func (l *lcovReader) da(srcLine int, value string) error {
	if l.file == "" {
		return malformed(srcLine, "DA before SF")
	}
	fields := strings.Split(value, ",")
	if len(fields) != 2 && len(fields) != 3 {
		return malformed(srcLine, "DA has %d fields, want line,hits[,checksum]", len(fields))
	}
	number, err := parseCount(fields[0], srcLine, "line", 1)
	if err != nil {
		return err
	}
	hits, err := parseCount(fields[1], srcLine, "hit count", 0)
	if err != nil {
		return err
	}
	return l.b.addNormalized(srcLine, l.file, number, hits)
}

// informationalLCOV lists the records this package accepts and does not read. A record outside the
// list is refused, so a misspelled DA cannot silently remove a line from the report.
func informationalLCOV(name string) bool {
	switch name {
	case "TN", "FN", "FNDA", "FNF", "FNH", "BRDA", "BRF", "BRH", "LF", "LH":
		return true
	default:
		return false
	}
}
