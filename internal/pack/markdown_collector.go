package pack

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
)

var (
	// ErrInvalidSource means that a source path is not a safe root-relative path.
	ErrInvalidSource = errors.New("invalid markdown source")
	// ErrCollectionLimit means that the configured file or byte bound was exceeded.
	ErrCollectionLimit = errors.New("markdown collection limit exceeded")
	// ErrSymlink means that a source contains a symbolic link.
	ErrSymlink = errors.New("markdown source contains symlink")
)

// Root is the small filesystem surface needed by Collector. Implementations must
// resolve names inside one already-authorized root.
type Root interface {
	Open(name string) (fs.File, error)
	ReadDir(name string) ([]fs.DirEntry, error)
}

type fsRoot struct{ fsys fs.FS }

// NewFSRoot adapts an injected root filesystem for collection.
func NewFSRoot(fsys fs.FS) Root { return fsRoot{fsys: fsys} }

func (r fsRoot) Open(name string) (fs.File, error) { return r.fsys.Open(name) }

func (r fsRoot) ReadDir(name string) ([]fs.DirEntry, error) { return fs.ReadDir(r.fsys, name) }

// OSRoot is a filesystem root that cannot open paths outside directory.
type OSRoot struct{ root *os.Root }

// NewOSRoot opens directory as the only filesystem root available to the collector.
func NewOSRoot(directory string) (*OSRoot, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("open markdown root: %w", err)
	}
	return &OSRoot{root: root}, nil
}

// Close releases the operating-system root.
func (r *OSRoot) Close() error { return r.root.Close() }

// Open opens a root-relative file below the operating-system root.
func (r *OSRoot) Open(name string) (fs.File, error) { return r.root.Open(name) }

// ReadDir reads directory entries below the operating-system root.
func (r *OSRoot) ReadDir(name string) ([]fs.DirEntry, error) {
	return fs.ReadDir(r.root.FS(), name)
}

// Limits bounds a collection before it can consume untrusted repository data.
type Limits struct {
	MaxFiles int
	MaxBytes int64
}

// Document is one collected Markdown document. Provenance is stable across runs.
type Document struct {
	Path       string
	Content    string
	Provenance string
	SHA256     [sha256.Size]byte
}

// Collector reads only Markdown documents below its injected root.
type Collector struct {
	root   Root
	limits Limits
}

// NewCollector constructs a bounded Markdown collector.
func NewCollector(root Root, limits Limits) (*Collector, error) {
	if root == nil {
		return nil, errors.New("markdown root is nil")
	}
	if limits.MaxFiles <= 0 || limits.MaxBytes <= 0 {
		return nil, errors.New("markdown limits must be positive")
	}
	return &Collector{root: root, limits: limits}, nil
}

// Collect returns documents from the supplied root-relative source paths in stable path order.
func (c *Collector) Collect(sources []string) ([]Document, error) {
	paths := make(map[string]struct{})
	for _, source := range sources {
		if err := validateSource(source); err != nil {
			return nil, err
		}
		if err := c.collectSource(source, paths); err != nil {
			return nil, err
		}
	}

	ordered := make([]string, 0, len(paths))
	for documentPath := range paths {
		ordered = append(ordered, documentPath)
	}
	sort.Strings(ordered)

	documents := make([]Document, 0, len(ordered))
	var total int64
	for _, documentPath := range ordered {
		document, bytesRead, err := c.readDocument(documentPath, c.limits.MaxBytes-total)
		if err != nil {
			return nil, err
		}
		total += bytesRead
		documents = append(documents, document)
	}
	return documents, nil
}

func (c *Collector) collectSource(source string, paths map[string]struct{}) error {
	entry, err := c.entry(source)
	if err != nil {
		return fmt.Errorf("inspect markdown source %q: %w", source, err)
	}
	if entry.Type()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrSymlink, source)
	}
	if !entry.IsDir() {
		if isMarkdown(entry.Name()) {
			return c.addPath(source, paths)
		}
		return nil
	}
	return c.walk(source, paths)
}

func (c *Collector) walk(directory string, paths map[string]struct{}) error {
	entries, err := c.root.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read markdown source %q: %w", directory, err)
	}
	for _, entry := range entries {
		name := path.Join(directory, entry.Name())
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymlink, name)
		}
		switch {
		case entry.IsDir():
			if err := c.walk(name, paths); err != nil {
				return err
			}
		case isMarkdown(entry.Name()):
			if err := c.addPath(name, paths); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Collector) entry(name string) (fs.DirEntry, error) {
	current := "."
	var found fs.DirEntry
	for _, component := range strings.Split(name, "/") {
		entries, err := c.root.ReadDir(current)
		if err != nil {
			return nil, err
		}
		found = nil
		for _, entry := range entries {
			if entry.Name() == component {
				found = entry
				break
			}
		}
		if found == nil {
			return nil, fs.ErrNotExist
		}
		if found.Type()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: %s", ErrSymlink, path.Join(path.Dir(current), component))
		}
		current = path.Join(current, component)
	}
	return found, nil
}

func (c *Collector) addPath(name string, paths map[string]struct{}) error {
	if _, exists := paths[name]; exists {
		return nil
	}
	if len(paths) >= c.limits.MaxFiles {
		return fmt.Errorf("%w: more than %d files", ErrCollectionLimit, c.limits.MaxFiles)
	}
	paths[name] = struct{}{}
	return nil
}

func (c *Collector) readDocument(name string, remaining int64) (document Document, bytesRead int64, err error) {
	file, err := c.root.Open(name)
	if err != nil {
		return Document{}, 0, fmt.Errorf("open markdown document %q: %w", name, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			closeErr = fmt.Errorf("close markdown document %q: %w", name, closeErr)
			err = errors.Join(err, closeErr)
			document = Document{}
			bytesRead = 0
		}
	}()
	content, err := io.ReadAll(io.LimitReader(file, remaining+1))
	if err != nil {
		return Document{}, 0, fmt.Errorf("read markdown document %q: %w", name, err)
	}
	if int64(len(content)) > remaining {
		return Document{}, 0, fmt.Errorf("%w: %s exceeds remaining byte budget", ErrCollectionLimit, name)
	}
	return Document{
		Path:       name,
		Content:    string(content),
		Provenance: "doc:" + name,
		SHA256:     sha256.Sum256(content),
	}, int64(len(content)), nil
}

func validateSource(source string) error {
	if source == "" || source == "." || strings.Contains(source, "\\") || !fs.ValidPath(source) || path.IsAbs(source) {
		return fmt.Errorf("%w: %q", ErrInvalidSource, source)
	}
	return nil
}

func isMarkdown(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "readme") || strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown")
}
