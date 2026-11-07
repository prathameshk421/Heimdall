package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/postgres"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/pack"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

const maxPackUploadBytes = 20 << 20

type packUploadHandler struct {
	tokens   mcp.Tokens
	config   config.Config
	uploader pack.Uploader
}

type packUploadRequest struct {
	Manifest   pack.Manifest `json:"manifest"`
	Ciphertext []byte        `json:"ciphertext"`
}

// uploadFailure is the HTTP status and client-facing message for a rejected upload.
type uploadFailure struct {
	status int
	msg    string
}

func newPackUploadHandler(d deps, tokens mcp.Tokens, blobs ports.BlobStore, pool *pgxpool.Pool) (http.Handler, error) {
	if len(d.configuration.PackIdentities) == 0 {
		return nil, nil
	}
	deleter, ok := blobs.(ports.BlobDeleter)
	if !ok {
		return nil, errors.New("pack blob store does not support deletion")
	}
	vault, err := pack.NewVault(blobs, deleter, postgres.NewStore(pool))
	if err != nil {
		return nil, fmt.Errorf("build pack vault: %w", err)
	}
	if tokens == nil {
		return nil, errors.New("pack upload token store is not available")
	}
	return packUploadHandler{tokens: tokens, config: d.configuration, uploader: vault}, nil
}

func (h packUploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if fail := h.accept(w, r); fail != nil {
		http.Error(w, fail.msg, fail.status)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// accept runs the upload pipeline: method, authentication, decoding, owner checks, verification and storage.
func (h packUploadHandler) accept(w http.ResponseWriter, r *http.Request) *uploadFailure {
	if r.Method != http.MethodPost {
		return &uploadFailure{http.StatusMethodNotAllowed, "method not allowed"}
	}
	person, fail := h.authenticate(r)
	if fail != nil {
		return fail
	}
	input, fail := readUploadRequest(w, r)
	if fail != nil {
		return fail
	}
	if input.Manifest.Person != person || len(input.Ciphertext) == 0 {
		return &uploadFailure{http.StatusForbidden, "pack owner does not match the authenticated person"}
	}
	if fail := h.verify(r, person, input); fail != nil {
		return fail
	}
	if err := h.uploader.Upload(r.Context(), person, input.Ciphertext, input.Manifest); err != nil {
		return &uploadFailure{http.StatusServiceUnavailable, "pack could not be stored"}
	}
	return nil
}

// authenticate resolves the bearer token to a mapped human person.
func (h packUploadHandler) authenticate(r *http.Request) (string, *uploadFailure) {
	token, ok := uploadBearer(r.Header.Get("Authorization"))
	if !ok {
		return "", &uploadFailure{http.StatusUnauthorized, "a bearer token is required"}
	}
	principal, err := h.tokens.Resolve(r.Context(), mcp.HashToken(token))
	if err != nil || !principal.Human || principal.Person == "" || principal.Agent != "" {
		return "", &uploadFailure{http.StatusUnauthorized, "a mapped human principal is required"}
	}
	return principal.Person, nil
}

// readUploadRequest reads a bounded body and decodes it strictly.
func readUploadRequest(w http.ResponseWriter, r *http.Request) (packUploadRequest, *uploadFailure) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPackUploadBytes))
	if err != nil {
		return packUploadRequest{}, &uploadFailure{http.StatusRequestEntityTooLarge, "pack upload body is invalid or too large"}
	}
	var input packUploadRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return packUploadRequest{}, &uploadFailure{http.StatusBadRequest, "pack upload body is invalid"}
	}
	return input, nil
}

// verify checks the upload's signature, encryption and section hashes against the person's configured keys.
func (h packUploadHandler) verify(r *http.Request, person string, input packUploadRequest) *uploadFailure {
	secrets, ok := h.config.PackIdentities[person]
	if !ok {
		return &uploadFailure{http.StatusForbidden, "pack identity is not configured for this person"}
	}
	publicText, err := h.config.Secret(secrets.SigningPublicKey)
	if err != nil {
		return &uploadFailure{http.StatusServiceUnavailable, "pack signing key is unavailable"}
	}
	publicKey, err := pack.ParseSigningPublicKey(publicText)
	if err != nil {
		return &uploadFailure{http.StatusServiceUnavailable, "pack signing key is invalid"}
	}
	identity, err := h.config.Secret(secrets.AgeIdentity)
	if err != nil {
		return &uploadFailure{http.StatusServiceUnavailable, "pack age identity is unavailable"}
	}
	stored := pack.StoredPack{Owner: person, Ciphertext: input.Ciphertext, Manifest: input.Manifest}
	if err := pack.VerifyEncryptedPack(r.Context(), stored, publicKey, pack.AgeCipher{Identity: identity}); err != nil {
		return &uploadFailure{http.StatusBadRequest, "pack signature, encryption, or section hashes are invalid"}
	}
	return nil
}

func uploadBearer(header string) (string, bool) {
	scheme, token, found := strings.Cut(header, " ")
	return token, found && strings.EqualFold(scheme, "Bearer") && token != "" && !strings.ContainsAny(token, " \t\r\n")
}
