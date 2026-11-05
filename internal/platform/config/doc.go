// Package config loads heimdall.toml and reads secrets from the secrets directory.
//
// # Purpose
//
// The server's infrastructure configuration (listen address, secrets_dir, the tracker-to-repo project
// map, the database and adapter endpoints, and the secret file names) and the only path from disk to a
// secret value. It deliberately does not parse heimdall.yaml (policy, in each repo), does not connect to
// anything, and does not decide what a secret is used for. A URL is never a secret; a secret is only ever
// named here, never inlined.
//
// # API
//
//   - Load(path) (Config, error): strict decode of the TOML file. Unknown keys are errors. Every problem
//     found is reported, joined with errors.Join, so one run shows them all.
//   - Config.Secret(name) (string, error): reads one file under SecretsDir. The value is trimmed of its
//     trailing newline. Errors never contain the value.
//   - Config.SecretMultiline(name) ([]byte, error): the same read for a multi-line secret such as a PEM key.
//     It allows newlines, caps the file at 16 KiB, and refuses a file whose mode has any group or other bit
//     (mode & 0o077 must be 0). The trailing newlines are removed.
//   - Config.ProjectFor(trackerProject) (Project, bool): the repo mapped to a tracker project.
//   - [database] url_secret, [jira] base_url, project_keys, email_secret, api_token_secret, [switch]
//     base_url, agent_id, agent_name, bridge_id, api_key_secret, registration_token_secret, and [github]
//     app_id_secret, installation_id_secret, private_key_secret: the live-run settings. Every secret key is
//     a file name under SecretsDir, never a value. A present section must be complete; an absent one is
//     left for the binary's missing-key check.
//   - Config.Selection(kind) (string, bool): the adapter name chosen in [vcs] provider, [tracker] provider,
//     [chat] bridge, [agent_runtime] provider or [memory] provider. Absent means unset; an empty value is
//     refused at Load. The registry package checks the name exists (registry.ValidateSelections).
//   - OnceSet.First(key) bool: true only the first time a key is offered. The caller uses it to log an
//     unmapped project once. The zero value is ready to use and it is safe for concurrent use.
//   - FilePath(args, getenv) (string, error): the --config value, else HEIMDALL_CONFIG. This package is
//     the only one allowed to read the environment (the lint rule exempts it).
//   - Getenv(name) string: os.Getenv for the binary's wiring, so cmd/* never names os.Getenv.
//
// # Invariants
//
//   - A loaded Config has a non-empty SecretsDir and Listen that parses as host:port.
//   - A selection, when present, is a non-empty string. Its kind is one of the five section names above.
//   - Every Project has a tracker_project, a repo shaped owner/name and a base_branch; tracker_project
//     values are unique.
//   - A present [database], [jira], [switch] or [github] section has every key. A base URL is absolute
//     https, or http only when the host is loopback. A project key matches ^[A-Z][A-Z0-9_]*$ and the list
//     has no duplicates. Every secret key is a single path element.
//   - Secret names are single path elements: no separators, no "..". A secret is a regular file whose
//     real location (after symlinks) stays inside SecretsDir. The read goes through os.Root, which
//     enforces the escape check.
//   - No secret value is ever stored in Config, logged, or put in an error. Only secret file names and
//     non-secret values (URLs, ids, names) appear in Config and in its errors.
//
// # Failure modes
//
//   - A bad or unknown key, a missing field, a duplicate project, a bad repo: Load returns an error and
//     the server refuses to start (the caller exits 1).
//   - A half-filled live section, an empty or non-loopback plain-http base URL, a malformed or duplicate
//     project key, or a secret key that is not one path element: Load names the key and refuses to start.
//   - A missing, empty, multi-line, non-regular or escaping secret file: Secret returns an error that
//     names the secret but not its value.
//   - A multi-line secret that is over 16 KiB or readable by group or others: SecretMultiline returns an
//     error that names the secret and its mode, never its content.
//   - A named pipe in the secrets directory: the open uses O_NONBLOCK, so the read does not wait for a
//     writer, and Stat then refuses it as not a regular file (Secret and SecretMultiline both).
//   - A missing config path (no --config and no HEIMDALL_CONFIG): FilePath returns an error; there is
//     no default path.
//
// # Tests
//
// config_test.go holds table tests for every Load refusal, the secret traversal, symlink-escape and
// empty-file refusals, a check that secret values never appear in errors, the OnceSet behaviour,
// FilePath, and the provider selections (read, absent, empty and unknown keys refused).
// sections_test.go covers the live sections: every key read, every missing or invalid key named, https and
// loopback-http URLs, project key shape and duplicates, secret file names, and every problem reported at once.
// secret_multiline_test.go covers SecretMultiline: a three-line PEM shape, owner-only modes accepted,
// group and world modes refused, a symlink out of the directory, the 16 KiB limit, and canary checks.
// Board: the server start in cmd/heimdall-server (run_test.go, live_config_test.go).
package config
