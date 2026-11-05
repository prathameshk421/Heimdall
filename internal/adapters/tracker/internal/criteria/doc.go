// Package criteria parses acceptance criteria out of ticket text (TECH_SPEC D50). Both tracker adapters use it:
// the file tracker on markdown, the Jira adapter on the markdown-like text its ADF walker renders.
//
// # Rules
//
//   - A section starts at a heading whose text is "Acceptance criteria" (case-insensitive, any level, an optional
//     trailing colon) and ends at the next heading.
//   - An item is a line starting with "-", "*", "+", or a number followed by "." or ")", then text.
//   - Indented items and wrapped lines join the item above them with a newline. A blank line ends an item. Other
//     text in the section is ignored.
//   - Ids are AC1..n in order, across all such sections. A ticket can hold at most 999 criteria (AC999).
//   - Hash is the SHA-256 of the item text, so an edit that keeps the text keeps the id (D50).
//
// # Invariants
//
//   - Parse is a pure function of its input. It reads no clock and no file.
//   - A missing section is not an error: the test-author asks for the criteria instead (D50).
//
// # Tests
//
// criteria_test.go is table-style: numbered and bullet items, heading variants, section end, nesting and wrapping,
// blank-line ends, several sections, hash stability and change, trimming, and the 999 limit.
package criteria
