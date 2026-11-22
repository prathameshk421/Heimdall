package coverage

import "strings"

// ParseGoCover reads a profile written by `go test -coverprofile`. Each block adds its count to every
// line it spans, so a multi-line statement counts as covered when the block ran. The end position is
// exclusive: a block that ends at column 1 of a later line leaves that line out. A record for the same
// line that appears twice adds up.
func ParseGoCover(data []byte, opt Options) (Result, error) {
	b, err := NewBuilder(opt)
	if err != nil {
		return Result{}, err
	}
	if err := b.CheckSize(data); err != nil {
		return Result{}, err
	}
	records := splitLines(data)
	mode, err := goCoverMode(records)
	if err != nil {
		return Result{}, err
	}
	for i, record := range records[1:] {
		if record == "" {
			continue
		}
		if err := addGoCoverRecord(b, mode, record, i+2); err != nil {
			return Result{}, err
		}
	}
	return b.Result(), nil
}

// goCoverMode reads the mode line, which must be the first line and must name a known mode.
func goCoverMode(records []string) (string, error) {
	const prefix = "mode: "
	if len(records) == 0 || !strings.HasPrefix(records[0], prefix) {
		return "", malformed(1, "the profile has no mode line")
	}
	mode := strings.TrimPrefix(records[0], prefix)
	switch mode {
	case "set", "count", "atomic":
		return mode, nil
	default:
		return "", malformed(1, "mode %q is not set, count or atomic", mode)
	}
}

// addGoCoverRecord adds one file:startLine.startCol,endLine.endCol statements count line.
func addGoCoverRecord(b *Builder, mode, record string, srcLine int) error {
	colon := strings.LastIndexByte(record, ':')
	if colon <= 0 || colon == len(record)-1 {
		return malformed(srcLine, "record %q has no file:span", record)
	}
	raw, span := record[:colon], record[colon+1:]
	fields := strings.Fields(span)
	if len(fields) != 3 {
		return malformed(srcLine, "record has %d fields after the file, want span statements count", len(fields))
	}
	start, end, err := parseGoSpan(fields[0], srcLine)
	if err != nil {
		return err
	}
	if _, err := parseCount(fields[1], srcLine, "statement count", 0); err != nil {
		return err
	}
	hits, err := parseCount(fields[2], srcLine, "hit count", 0)
	if err != nil {
		return err
	}
	if mode == "set" && hits > 1 {
		return malformed(srcLine, "mode set counts a block once, got %d", hits)
	}
	file, err := b.file(raw, srcLine)
	if err != nil {
		return err
	}
	for line := start; line <= end; line++ {
		if err := b.addNormalized(srcLine, file, line, hits); err != nil {
			return err
		}
	}
	return nil
}

// parseGoSpan reads startLine.startCol,endLine.endCol and returns the line range it covers.
func parseGoSpan(span string, srcLine int) (int, int, error) {
	parts := strings.Split(span, ",")
	if len(parts) != 2 {
		return 0, 0, malformed(srcLine, "span %q is not startLine.startCol,endLine.endCol", span)
	}
	startLine, startCol, err := parseGoPos(parts[0], srcLine)
	if err != nil {
		return 0, 0, err
	}
	endLine, endCol, err := parseGoPos(parts[1], srcLine)
	if err != nil {
		return 0, 0, err
	}
	if endLine < startLine || (endLine == startLine && endCol < startCol) {
		return 0, 0, malformed(srcLine, "span %q ends before it starts", span)
	}
	if endLine > MaxLine {
		return 0, 0, malformed(srcLine, "line %d is above %d", endLine, MaxLine)
	}
	// The end position is exclusive. A block that ends at column 1 of a later line holds no character of
	// that line, so the line is not part of the block: Go writes this for a block that stops at a closing
	// brace on its own line, and `go tool cover -html` closes its span at the start of that line.
	if endLine > startLine && endCol <= 1 {
		endLine--
	}
	return startLine, endLine, nil
}

// parseGoPos reads line.column. A column may be zero; a line may not.
func parseGoPos(pos string, srcLine int) (int, int, error) {
	lineText, columnText, ok := strings.Cut(pos, ".")
	if !ok {
		return 0, 0, malformed(srcLine, "position %q is not line.column", pos)
	}
	line, err := parseCount(lineText, srcLine, "line", 1)
	if err != nil {
		return 0, 0, err
	}
	column, err := parseCount(columnText, srcLine, "column", 0)
	if err != nil {
		return 0, 0, err
	}
	return line, column, nil
}
