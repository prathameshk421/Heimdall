---
applies_to: []
check: "Does every symbol the change removes or renames still have no reference left in the tree?"
proof: "Search the whole tree for the old name and show that the only hits left are in the change itself."
kind: convention
source: heimdall lens template: correctness
status: active
---
Removing a name is a promise that nothing uses it. A reference left behind is a build failure at best, and a silent
fallback to another code path at worst.

Search the tree for the old name, not only for the files in the diff: a rename leaves the old name in a comment, a
string, a template or a configuration file. A hit in a comment is harmless; a hit in code is a caller the change
forgot.
