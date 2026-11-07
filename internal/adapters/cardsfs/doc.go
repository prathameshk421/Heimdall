// Package cardsfs serves personal context cards: small text files a developer keeps about what they own, the rules
// that must hold in their code and what past incidents taught them.
//
// Layout: <dir>/<person>/<file>. Only .md, .yaml, .yml and .txt files directly inside a person's folder are cards.
// Every access goes through os.Root, so a name or a symlink cannot leave the person's folder. The person is chosen
// by the caller from the authenticated agent, never from a tool argument.
//
// Failure modes: a missing person folder lists nothing; an unsafe name, an unknown file, a symlink out of the folder
// or a card over MaxCardBytes is an error and returns no content.
package cardsfs
