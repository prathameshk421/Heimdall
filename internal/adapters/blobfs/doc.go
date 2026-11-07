// Package blobfs stores ports.BlobStore content beneath a configured filesystem root.
// References contain only a validated SHA-256 digest; class directories carry the
// retention hint, and temporary files are renamed into place only after syncing.
// Get searches the class directories because the public reference intentionally
// identifies content independently of its retention class.
package blobfs
