package codegraph

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// Limits that keep one build inside a fixed memory and time budget.
const (
	maxTarballBytes = 512 << 20
	maxSourceBytes  = 128 << 20
	maxFileBytes    = 1 << 20
	maxFiles        = 30000
	maxUnpackBytes  = 2 << 30
	binarySniff     = 8000
)

// Build reads a gzip tar in the GitHub layout, with one top folder that it strips, and indexes the source files.
func Build(tarball io.Reader) (*Index, error) {
	packed := &io.LimitedReader{R: tarball, N: maxTarballBytes}
	gz, err := gzip.NewReader(packed)
	if err != nil {
		return nil, fmt.Errorf("open tarball: %w", err)
	}
	unpacked := &io.LimitedReader{R: gz, N: maxUnpackBytes}
	files, truncated, err := readTar(tar.NewReader(unpacked))
	if err != nil {
		if packed.N > 0 && unpacked.N > 0 {
			return nil, err
		}
		truncated = true
	}
	return newIndex(files, truncated), nil
}

func readTar(tr *tar.Reader) (map[string]string, bool, error) {
	files := map[string]string{}
	total := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return files, false, nil
		}
		if err != nil {
			return files, false, fmt.Errorf("read tarball: %w", err)
		}
		name, ok := sourceName(hdr)
		if !ok {
			continue
		}
		if len(files) >= maxFiles || total+int(hdr.Size) > maxSourceBytes {
			return files, true, nil
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxFileBytes+1))
		if err != nil {
			return files, false, fmt.Errorf("read %s: %w", name, err)
		}
		if len(data) > maxFileBytes || bytes.IndexByte(data[:min(len(data), binarySniff)], 0) >= 0 {
			continue
		}
		files[name] = string(data)
		total += len(data)
	}
}

// sourceName returns the path without the top folder, and false for an entry that is not wanted source.
func sourceName(hdr *tar.Header) (string, bool) {
	if hdr.Typeflag != tar.TypeReg || hdr.Size > maxFileBytes {
		return "", false
	}
	_, rest, found := strings.Cut(path.Clean(strings.TrimPrefix(hdr.Name, "./")), "/")
	if !found || rest == "" || strings.HasPrefix(rest, "/") || strings.Contains(rest, "..") {
		return "", false
	}
	return rest, wanted(rest)
}

func wanted(name string) bool {
	for _, part := range strings.Split(path.Dir(name), "/") {
		switch part {
		case "vendor", "node_modules", "dist", "build", "target", ".git", "__pycache__", "venv", ".venv":
			return false
		}
	}
	base := strings.ToLower(path.Base(name))
	if base == "go.sum" || strings.HasSuffix(base, ".lock") || strings.HasSuffix(base, ".min.js") || strings.HasSuffix(base, ".min.css") {
		return false
	}
	switch strings.TrimPrefix(path.Ext(base), ".") {
	case "png", "jpg", "jpeg", "gif", "svg", "ico", "webp", "bmp", "mp4", "mov", "avi", "mp3", "wav", "ogg", "pdf",
		"zip", "gz", "tar", "tgz", "7z", "woff", "woff2", "ttf", "otf", "eot", "jar", "class", "so", "dll", "dylib",
		"exe", "bin", "o", "a", "pyc", "map", "wasm":
		return false
	}
	return !strings.HasPrefix(base, "package-lock.") && base != "pnpm-lock.yaml"
}
