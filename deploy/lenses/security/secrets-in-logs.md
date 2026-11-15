---
applies_to: []
check: "Does no credential, token or personal datum reach a log line, an error message or a URL?"
proof: "Search the change for the name of the secret and show that it never reaches a log, an error or a query string."
kind: convention
source: heimdall lens template: security
status: active
---
A secret in a log is a secret in every log store, every backup and every support ticket. A token in a URL is a token
in the browser history and in the access log of the proxy.

Logs are kept longer than the secret lives and are read by more people. Read the lines the change writes and name
what each one carries. When a value is needed for support, log its shape and its last few characters, never the
value.
