// Package poll holds the overlap window and the dedupe shared by the tracker adapters (TECH_SPEC 13, D97, V55).
//
// # Why an overlap
//
// JQL compares dates to the minute, and the search index can lag behind a write. A poll that starts exactly at
// its cursor can therefore miss a change that was indexed late. Every poll starts ten minutes before the cursor,
// so a late change is seen by a later poll, and the dedupe keeps the repeats from triggering twice.
//
// # API
//
//   - Overlap: 10 minutes.
//   - WindowStart(cursor, now): the start of a poll. An empty cursor starts one overlap before now.
//   - Format(t): the cursor for a poll time (RFC 3339, UTC).
//   - Seen: the (ticket key, changelog id) pairs already reported, with the time each was written. Observe reports
//     true once per pair. Forget drops pairs written before the window start, which a later poll never reads again.
//
// # Tests
//
// poll_test.go: the window with and without a cursor, a refused cursor, the overlap length, the repeat, keying by
// both parts, and Forget.
package poll
