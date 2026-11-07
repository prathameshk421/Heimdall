package testset

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// TreeHash returns the git tree hash that dir has once the submission's files are committed under it. It is the
// hash `git rev-parse <commit>:<dir>` prints for a commit that holds exactly these files, so the verified tests can
// be compared with the committed tree. Every file has mode 100644, as the commit writes them. Each file must lie
// under dir.
func (s Submission) TreeHash(dir string) (string, error) {
	if len(s.Files) == 0 {
		return "", fmt.Errorf("tree hash of %s: the submission has no files", dir)
	}
	prefix := strings.TrimSuffix(dir, "/") + "/"
	root := &treeNode{children: map[string]*treeNode{}}
	for _, f := range s.Files {
		rel, ok := strings.CutPrefix(f.Path, prefix)
		if !ok || rel == "" {
			return "", fmt.Errorf("tree hash of %s: file %q is outside it", dir, f.Path)
		}
		if err := root.add(strings.Split(rel, "/"), f.Content); err != nil {
			return "", fmt.Errorf("tree hash of %s: file %q: %w", dir, f.Path, err)
		}
	}
	return hex.EncodeToString(root.hash()), nil
}

// treeNode is one directory of the tree being hashed. A file is a node with a blob and no children.
type treeNode struct {
	children map[string]*treeNode
	blob     []byte
	isFile   bool
}

func (n *treeNode) add(parts []string, content string) error {
	name := parts[0]
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("path element %q is not a name", name)
	}
	if len(parts) == 1 {
		if _, exists := n.children[name]; exists {
			return fmt.Errorf("%q is named twice", name)
		}
		n.children[name] = &treeNode{isFile: true, blob: []byte(content)}
		return nil
	}
	child, exists := n.children[name]
	if !exists {
		child = &treeNode{children: map[string]*treeNode{}}
		n.children[name] = child
	}
	if child.isFile {
		return fmt.Errorf("%q is both a file and a directory", name)
	}
	return child.add(parts[1:], content)
}

// hash is the git object id of the node: a blob for a file, a tree for a directory.
func (n *treeNode) hash() []byte {
	if n.isFile {
		return gitObject("blob", n.blob)
	}
	names := make([]string, 0, len(n.children))
	for name := range n.children {
		names = append(names, name)
	}
	// Git sorts tree entries by name with a directory compared as if its name ended in "/".
	slices.SortFunc(names, func(a, b string) int {
		return strings.Compare(sortKey(a, n.children[a]), sortKey(b, n.children[b]))
	})
	var body []byte
	for _, name := range names {
		child := n.children[name]
		mode := "40000"
		if child.isFile {
			mode = "100644"
		}
		body = append(body, mode+" "+name+"\x00"...)
		body = append(body, child.hash()...)
	}
	return gitObject("tree", body)
}

func sortKey(name string, n *treeNode) string {
	if n.isFile {
		return name
	}
	return name + "/"
}

func gitObject(kind string, body []byte) []byte {
	h := sha1.New()
	_, _ = h.Write([]byte(kind + " " + strconv.Itoa(len(body)) + "\x00"))
	_, _ = h.Write(body)
	return h.Sum(nil)
}
