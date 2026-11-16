// Package secretscan finds credentials and direct identifiers in text and redacts them without ever returning the
// matched values (AGENT_INTERACTION_ARCH 10, H9). It embeds the upstream gitleaks rules plus Heimdall's own PII and
// deny-list rules. The board uses it on room posts, the knowledge files use it on staged text. It was the scanner of
// the pack pipeline, moved here before that pipeline is deleted.
package secretscan
