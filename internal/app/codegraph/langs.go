package codegraph

import (
	"regexp"
	"strings"
)

const (
	jsMods   = `(?:export\s+)?(?:default\s+)?`
	javaMods = `(?:(?:public|private|protected|static|final|abstract|synchronized)\s+)`
	csMods   = `(?:(?:public|private|protected|internal|static|sealed|abstract|partial|virtual|override|async)\s+)`
)

// addLanguages registers the line patterns. Each pattern has one capture group for the name, or several where one of
// them is set.
func (t *tagger) addLanguages() {
	add := func(lang string, exts string, rules ...[2]string) {
		for _, e := range strings.Fields(exts) {
			t.ext[e] = lang
		}
		for _, r := range rules {
			t.langs[lang] = append(t.langs[lang], rule{regexp.MustCompile(r[1]), r[0]})
		}
	}
	add("py", "py", [2]string{"func", `^\s*(?:async\s+)?def\s+(\w+)`}, [2]string{"type", `^\s*class\s+(\w+)`})
	add("js", "js jsx ts tsx mjs cjs",
		[2]string{"func", `^\s*` + jsMods + `(?:async\s+)?function\s*\*?\s*(\w+)`},
		[2]string{"type", `^\s*` + jsMods + `(?:abstract\s+)?(?:class|interface|enum)\s+(\w+)`},
		[2]string{"type", `^\s*` + jsMods + `type\s+(\w+)\s*(?:<[^=]*>)?\s*=`},
		[2]string{"func", `^\s*` + jsMods + `(?:const|let|var)\s+(\w+)\s*(?::[^=]+)?=\s*(?:async\s*)?(?:\([^)]*\)|\w+)\s*(?::[^=]+)?=>`},
		[2]string{"method", `^\s+(?:(?:public|private|protected|static|async|readonly|override)\s+)*(\w+)\s*(?:<[^>]*>)?\([^)]*\)\s*(?::\s*[\w<>\[\]|, .]+)?\s*\{`})
	add("java", "java", [2]string{"type", `^\s*` + javaMods + `*(?:class|interface|enum|record)\s+(\w+)`},
		[2]string{"method", `^\s*` + javaMods + `+[\w<>\[\],.? ]+\s+(\w+)\s*\([^;]*$`})
	add("kt", "kt kts",
		[2]string{"func", `^\s*(?:(?:public|private|internal|protected|override|open|abstract|suspend|inline)\s+)*fun\s+(?:<[^>]+>\s*)?(?:[\w.<>]+\.)?(\w+)\s*\(`},
		[2]string{"type", `^\s*(?:(?:public|private|internal|data|sealed|open|abstract|enum)\s+)*(?:class|interface|object)\s+(\w+)`})
	add("rb", "rb", [2]string{"func", `^\s*def\s+(?:self\.)?(\w+)`}, [2]string{"type", `^\s*(?:class|module)\s+(\w+)`})
	add("rs", "rs",
		[2]string{"func", `^\s*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?(?:unsafe\s+)?fn\s+(\w+)`},
		[2]string{"type", `^\s*(?:pub(?:\([^)]*\))?\s+)?(?:struct|enum|trait|type|mod)\s+(\w+)`})
	add("cs", "cs", [2]string{"type", `^\s*` + csMods + `*(?:class|interface|struct|enum|record)\s+(\w+)`},
		[2]string{"method", `^\s*` + csMods + `+[\w<>\[\],.? ]+\s+(\w+)\s*\(`})
	add("php", "php", [2]string{"func", `^\s*(?:(?:public|private|protected|static|abstract|final)\s+)*function\s+&?(\w+)`},
		[2]string{"type", `^\s*(?:abstract\s+|final\s+)?(?:class|interface|trait|enum)\s+(\w+)`})
	add("c", "c h cpp cc cxx hpp hh",
		[2]string{"type", `^\s*(?:typedef\s+)?(?:struct|enum|union|class)\s+(\w+)`},
		[2]string{"const", `^#\s*define\s+(\w+)`},
		[2]string{"func", `^[A-Za-z_][\w \t\*&:<>,]*[\s\*&]([A-Za-z_]\w*)\s*\([^;]*$`})
	add("sh", "sh bash zsh", [2]string{"func", `^\s*(?:function\s+(\w+)|(\w+)\s*\(\))`})
	add("swift", "swift",
		[2]string{"func", `^\s*(?:(?:public|private|internal|fileprivate|open|static|final|override|mutating)\s+)*func\s+(\w+)`},
		[2]string{"type", `^\s*(?:(?:public|private|internal|fileprivate|open|final)\s+)*(?:class|struct|enum|protocol|extension|actor)\s+(\w+)`})
}
