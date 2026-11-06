// Package rootlessrunner populates the read-only module cache used by isolated execution jobs. A warm job never
// runs repository code: it only reads the archive, and every request goes through the exact-host
// fetch boundary, so the container jobs keep --network=none and no caller can widen the network.
package rootlessrunner

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	// DefaultModuleHost is the package registry the Go default profile warms from
	// (TECH_SPEC 12.2: the fetch job is the only job with egress, allowlisted to the registries the
	// profile needs plus its fetch_hosts).
	DefaultModuleHost = "proxy.golang.org"
	// maxWarmModules bounds one warm job, so one repository cannot turn the boundary into a queue.
	maxWarmModules = 800
	// maxWarmWarnings bounds the warm summary.
	maxWarmWarnings = 8
	// go.mod and go.sum readers are bounded independently of the archive limit.
	maxGomodBytes = 4 << 20
	maxGoSumBytes = 4 << 20
	// maxWarmArchive is the uncompressed tar budget of one warm job. It is separate from the
	// compressed 64 MiB input bound and from the fetcher's per-request bound.
	maxWarmArchive = 512 << 20
	maxTarEntries  = 20000
	// maxWarmBudget is the total staged-bytes budget of one warm job.
	maxWarmBudget            = 2 << 30
	maxModuleFileBytes int64 = 64 << 20
)

// Warm boundary errors. The runner maps them onto job outcomes, never onto silent retries.
var (
	ErrProfileNotFound     = errors.New("warm profile is not promoted on this runner")
	ErrFetchHostDisallowed = errors.New("warm fetch target host is not allowlisted")
	ErrNoModules           = errors.New("warm found no Go module definitions to stage")
	ErrWarmBudget          = errors.New("warm job exceeded its staged-bytes budget")
)

// WarmSummary is the bounded result of one warm job.
type WarmSummary struct {
	Modules  []string `json:"modules,omitempty"`
	Bytes    int64    `json:"bytes"`
	Skipped  int      `json:"skipped"`
	Warnings []string `json:"warnings,omitempty"`
}

// WarmFetcher is the network boundary a warm job uses. It must refuse every host outside the exact
// allowlist of the call, so a module's own URLs cannot widen the job's reach.
type WarmFetcher interface {
	Fetch(ctx context.Context, hosts []string, rawURL string) ([]byte, error)
}

// Warmer stages Go module archives into one module cache directory.
type Warmer struct {
	Boundary WarmFetcher // zero value uses the exact-host Fetcher
	Cache    string
	Hosts    []string
}

// moduleVersion is one go.sum or go.mod module pin.
type moduleVersion struct{ Module, Version string }

func (m moduleVersion) Name() string    { return m.Module }
func (m moduleVersion) String() string  { return m.Module + "@" + m.Version }
func (m moduleVersion) dirName() string { return escapedModule(m.Module) + "@" + m.Version }

// Warm reads the archive, resolves the module set, downloads every module through the boundary,
// verifies the zips against the repository's go.sum answers and stages the module cache. It is
// idempotent: an already-staged module is skipped, so a replay changes nothing.
func (w Warmer) Warm(ctx context.Context, archive io.Reader) (WarmSummary, error) {
	if w.Cache == "" {
		return WarmSummary{}, errors.New("warm module cache directory is required")
	}
	if len(w.Hosts) == 0 {
		return WarmSummary{}, fmt.Errorf("%w: the warm job must name exact fetch hosts", ErrFetchHostDisallowed)
	}
	if err := validWarmHosts(w.Hosts); err != nil {
		return WarmSummary{}, err
	}
	bound, limitErr := readAllBounded(archive, 64<<20)
	if limitErr != nil {
		return WarmSummary{}, limitErr
	}
	bundle, err := readBundle(bytes.NewReader(bound))
	if err != nil {
		return WarmSummary{}, err
	}
	if bundle.vendored {
		return WarmSummary{Warnings: []string{"repository is vendored, so no module cache staging was needed"}}, nil
	}
	modules := bundle.modules()
	if len(modules) == 0 {
		return WarmSummary{Warnings: bundle.warnings}, ErrNoModules
	}
	summary := WarmSummary{Warnings: bundle.warnings}
	seen := make(map[string]struct{}, len(modules))
	for _, m := range modules {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return summary, fmt.Errorf("warm job cancelled: %w", ctxErr)
		}
		staged, err := w.stage(ctx, m, bundle.zipHashes[m.String()], seen, &summary)
		if err != nil {
			return summary, err
		}
		if staged {
			summary.Modules = append(summary.Modules, m.String())
		} else {
			summary.Skipped++
		}
	}
	return summary, nil
}

func readAllBounded(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read warm archive: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("warm archive exceeds %d bytes", limit)
	}
	return data, nil
}

// warm fetches and stages modules to the runner's module cache with its fetch boundary.
func warmerFor(fetch WarmFetcher, cache string, hosts []string) Warmer {
	boundary := fetch
	if boundary == nil {
		boundary = Fetcher{} // the exact-host fetch boundary
	}
	return Warmer{Boundary: boundary, Cache: cache, Hosts: hosts}
}

// Warm resolves the profile's host rule and the module cache directory, then warms.
func (r *Runner) Warm(ctx context.Context, profileName string, archive io.Reader) (WarmSummary, error) {
	hosts, warnings, err := r.warmHosts(profileName)
	if err != nil {
		return WarmSummary{}, err
	}
	summary, warmErr := warmerFor(nil, r.moduleCache(), hosts).Warm(ctx, archive)
	if warmErr == nil {
		summary.Warnings = appendBounded(summary.Warnings, warnings...)
	}
	return summary, warmErr
}

// warmHosts resolves the exact host allowlist of one warm job. The built-in go profile warms from
// the Go registry; a promoted profile warms from its own fetch_hosts — a profile without any falls
// back to the Go registry with a warning.
func (r *Runner) warmHosts(profileName string) ([]string, []string, error) {
	if profileName == "" || profileName == "go" {
		return []string{DefaultModuleHost}, nil, nil
	}
	spec, named := r.specNamed(profileName)
	if !named {
		return nil, nil, ErrProfileNotFound
	}
	var warnings []string
	hosts := slices.Clone(spec.FetchHosts)
	if len(hosts) == 0 {
		return []string{DefaultModuleHost}, []string{"profile " + profileName + " names no fetch_hosts, so the Go registry is used"}, nil
	}
	if err := validWarmHosts(hosts); err != nil {
		return nil, nil, err
	}
	return hosts, warnings, nil
}

// validWarmHosts refuses every host that is not an exact DNS hostname, so the fetch boundary
// cannot be widened through wildcards, paths or ports.
func validWarmHosts(hosts []string) error {
	for _, host := range hosts {
		u, err := parseURL("https://" + host)
		if err != nil || u.Host != host || u.Path != "" || u.User != nil || strings.ContainsAny(host, "*:/\\") {
			return fmt.Errorf("%w: %q is not an exact host", ErrFetchHostDisallowed, host)
		}
	}
	return nil
}

func parseURL(raw string) (*url.URL, error) { return url.Parse(raw) }

func (r *Runner) specNamed(name string) (RepoProfile, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.active[name]
	return spec, ok
}

// appendBounded appends warnings without exceeding the summary bound.
func appendBounded(list []string, more ...string) []string {
	for _, item := range more {
		if len(list) >= maxWarmWarnings {
			return list
		}
		list = append(list, item)
	}
	return list
}

// ---------------------------------------------------------------------------
// Archive scanning

type warmBundle struct {
	gomod     []byte
	gosum     []byte
	vendored  bool
	zipHashes map[string]string
	warnings  []string
}

func (b *warmBundle) modules() []moduleVersion {
	var out []moduleVersion
	if len(b.gosum) > 0 {
		out = collectGoSumVersions(b.gosum)
	} else {
		out = parseGomodRequires(string(b.gomod))
		if len(out) > 0 {
			b.warnings = appendBounded(b.warnings, "go.sum is missing, so the module set comes from go.mod requires and may be incomplete")
		}
	}
	slices.SortFunc(out, func(a, b moduleVersion) int {
		if c := strings.Compare(a.Module, b.Module); c != 0 {
			return c
		}
		return strings.Compare(a.Version, b.Version)
	})
	if len(out) > maxWarmModules {
		return nil
	}
	return out
}

// readBundle finds the shallowest vendor manifest, go.sum and go.mod members of the archive, with
// per-file and per-entry bounds.
func readBundle(archive io.Reader) (warmBundle, error) {
	var bundle warmBundle
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return bundle, fmt.Errorf("open warm archive: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tarReader := tar.NewReader(io.LimitReader(gz, maxWarmArchive))
	for count := 0; count < maxTarEntries; count++ {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return bundle, finishBundle(&bundle)
		}
		if err != nil {
			return bundle, fmt.Errorf("read warm archive: %w", err)
		}
		if err := readBundleMember(tarReader, header, &bundle); err != nil {
			return bundle, err
		}
	}
	return bundle, fmt.Errorf("warm archive holds more than %d file entries", maxTarEntries)
}

func readBundleMember(reader *tar.Reader, header *tar.Header, bundle *warmBundle) error {
	if header.Typeflag != tar.TypeReg {
		return nil
	}
	name := cleanMemberName(header.Name)
	if isVendorManifest(name) {
		bundle.vendored = true
	}
	if depthOf(name) != 1 || strings.Contains(name, "/vendor/") {
		return nil
	}
	var err error
	switch name {
	case "go.sum":
		if bundle.gosum == nil {
			bundle.gosum, err = readBoundedTar(reader, maxGoSumBytes)
		}
	case "go.mod":
		if bundle.gomod == nil {
			bundle.gomod, err = readBoundedTar(reader, maxGomodBytes)
		}
	}
	return err
}

func finishBundle(b *warmBundle) error {
	if b.vendored {
		return nil
	}
	b.zipHashes = goSumHashes(b.gosum)
	if len(b.gosum) == 0 && len(b.gomod) == 0 {
		return ErrNoModules
	}
	return nil
}

func cleanMemberName(name string) string {
	name = strings.TrimPrefix(name, "../")
	name = strings.TrimPrefix(name, "./")
	return strings.Trim(name, "/")
}

func isVendorManifest(name string) bool {
	return name == "vendor/modules.txt"
}

func depthOf(name string) int {
	return strings.Count(name, "/") + 1
}

func readBoundedTar(reader *tar.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read warm archive member: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("warm archive member exceeds %d bytes", limit)
	}
	return data, nil
}

// ---------------------------------------------------------------------------
// go.sum and go.mod parsing

var moduleShape = regexp.MustCompile(`^[A-Za-z0-9._+/-]+$`)
var versionShape = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9][0-9A-Za-z.+-]*$`)
var requireLine = regexp.MustCompile(`^[ \t]*require[ \t]+([^\s]+)[ \t]+(v[^\s]+)`)

func validModulePath(module string) bool {
	return module != "" && len(module) <= 253 && moduleShape.MatchString(module) &&
		!strings.Contains(module, "..")
}

func validModuleVersion(version string) bool {
	return len(version) <= 64 && versionShape.MatchString(version)
}

func collectGoSumVersions(data []byte) []moduleVersion {
	seen := make(map[string]struct{}, 128)
	out := make([]moduleVersion, 0, 128)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 3 {
			continue
		}
		key := fields[0] + "\x00" + fields[1]
		if _, dup := seen[key]; dup {
			continue
		}
		module, version := fields[0], fields[1]
		if !validModulePath(module) || !validModuleVersion(version) {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, moduleVersion{Module: module, Version: version})
	}
	return out
}

// goSumHashes records the h1: module-zip hashes keyed by "<module>@<version>", so every download is
// answered against the checksum the repository carries. The <version>/go.mod rows name the go.mod
// digest instead and are not the zip checksum.
func goSumHashes(data []byte) map[string]string {
	hashes := make(map[string]string, 128)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 3 || !strings.HasPrefix(fields[2], "h1:") {
			continue
		}
		if strings.HasSuffix(fields[1], "/go.mod") {
			continue
		}
		hashes[fields[0]+"@"+fields[1]] = fields[2]
	}
	return hashes
}

// parseGomodRequires reads the require directives of the root go.mod. It is the fallback for
// repositories without go.sum, and the bundle records a warning for those.
func parseGomodRequires(gomod string) []moduleVersion {
	seen := make(map[string]struct{}, 64)
	out := make([]moduleVersion, 0, 64)
	inBlock := false
	for _, line := range strings.Split(gomod, "\n") {
		clean := stripComment(line)
		switch {
		case strings.HasPrefix(strings.TrimSpace(clean), "require ("):
			inBlock = true
		case inBlock && strings.HasPrefix(strings.TrimSpace(clean), ")"):
			inBlock = false
		default:
			if m, ok := moduleVersionOf(clean, inBlock); ok {
				key := m.Name() + "\x00" + m.Version
				if _, dup := seen[key]; !dup {
					seen[key] = struct{}{}
					out = append(out, m)
				}
			}
		}
	}
	return out
}

func stripComment(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}

// moduleVersionOf accepts one require line when the module and version shapes hold. In a require
// block the line is two tokens; a single-line require starts with the keyword.
func moduleVersionOf(line string, inBlock bool) (moduleVersion, bool) {
	var fields []string
	if inBlock {
		fields = strings.Fields(strings.TrimSpace(line))
	} else {
		matches := requireLine.FindStringSubmatch(line)
		if len(matches) != 3 {
			return moduleVersion{}, false
		}
		fields = matches[1:]
	}
	if len(fields) != 2 {
		return moduleVersion{}, false
	}
	if !validModulePath(fields[0]) || !validModuleVersion(fields[1]) {
		return moduleVersion{}, false
	}
	return moduleVersion{Module: fields[0], Version: fields[1]}, true
}

// ---------------------------------------------------------------------------
// Module cache layout and staging

// encodedModule escapes a module path for the module cache the way the Go toolchain does: each
// uppercase letter becomes an exclamation mark plus the lowercase letter.
func encodedModule(module string) (string, error) {
	var out strings.Builder
	for _, r := range module {
		switch {
		case r >= 'A' && r <= 'Z':
			out.WriteByte('!')
			out.WriteRune(r + ('a' - 'A'))
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-', r == '_', r == '/', r == '+':
			out.WriteRune(r)
		default:
			return "", fmt.Errorf("module path %q has no cache-safe form", module)
		}
	}
	escaped := out.String()
	if escaped == "" || strings.Contains(escaped, "..") {
		return "", fmt.Errorf("module path %q has no cache-safe form", module)
	}
	return escaped, nil
}

func escapedModule(module string) string {
	escaped, err := encodedModule(module)
	if err != nil {
		return module // the caller validates the shape before writing anything
	}
	return escaped
}

// stage fetches one module and writes the module cache under this warmer. It returns true when
// something was staged, false when the module already sat in the cache (so replays are no-ops).
func (w Warmer) stage(ctx context.Context, m moduleVersion, expected string, seen map[string]struct{}, summary *WarmSummary) (bool, error) {
	key := m.Module + "\x00" + m.Version
	if _, done := seen[key]; done {
		return false, nil
	}
	seen[key] = struct{}{}
	if summary.Bytes > maxWarmBudget {
		return false, ErrWarmBudget
	}
	zipBytes, err := w.fetchZip(ctx, m, expected, summary)
	if err != nil {
		return false, err
	}
	escaped := m.dirName()
	downloadDir := filepath.Join(w.Cache, "cache", "download", escapedModule(m.Module), "@v")
	discarded := filepath.Join(w.Cache, escaped+"@"+m.Version)
	_ = discarded
	if fileExists(filepath.Join(downloadDir, m.Version+".zip")) && dirExists(discarded) {
		summary.Skipped++
		return false, nil
	}
	if err := w.writeModuleCache(escapedModule(m.Module), m.Version, m.Module, zipBytes); err != nil {
		return false, err
	}
	summary.Bytes += int64(len(zipBytes))
	return true, nil
}

// fetchZip downloads one module zip through the exact-host boundary and, when go.sum carries the
// module's h1: answer, verifies it before anything lands in the cache.
func (w Warmer) fetchZip(ctx context.Context, m moduleVersion, expected string, summary *WarmSummary) ([]byte, error) {
	escapedModule, err := encodedModule(m.Module)
	if err != nil {
		return nil, err
	}
	url := "https://" + w.Hosts[0] + "/" + escapedModule + "/@v/" + m.Version + ".zip"
	zipBytes, err := w.fetch(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch module %s: %w", m.String(), err)
	}
	if expected == "" {
		summary.Warnings = appendBounded(summary.Warnings, "module "+m.String()+" staged without a verified checksum")
		return zipBytes, nil
	}
	got, hashErr := hashModuleZip(zipBytes)
	if hashErr != nil {
		return nil, fmt.Errorf("hash module %s: %w", m.String(), hashErr)
	}
	if got != expected {
		return nil, fmt.Errorf("module %s does not match the repository checksum answer", m.String())
	}
	return zipBytes, nil
}

func (w Warmer) fetch(ctx context.Context, url string) ([]byte, error) {
	if w.Boundary == nil {
		return Fetcher{}.Fetch(ctx, w.Hosts, url)
	}
	return w.Boundary.Fetch(ctx, w.Hosts, url)
}

// writeModuleCache writes the download-cache entries and the extracted module tree. Every entry is
// published by rename, so a crash never leaves a half-written cache entry.
func (w Warmer) writeModuleCache(escapedModuleDir, version, module string, zipBytes []byte) error {
	downloadDir := filepath.Join(w.Cache, "cache", "download", escapedModuleDir, "@v")
	extracted := filepath.Join(w.Cache, escapedModuleDir+"@"+version)
	if err := prepareDir(downloadDir); err != nil {
		return fmt.Errorf("download cache directory: %w", err)
	}
	if !fileExists(filepath.Join(downloadDir, version+".zip")) {
		if err := publishFile(filepath.Join(downloadDir, version+".zip"), zipBytes); err != nil {
			return fmt.Errorf("stage module zip: %w", err)
		}
		if got, err := hashModuleZip(zipBytes); err == nil {
			if err := publishFile(filepath.Join(downloadDir, version+".ziphash"), []byte(got)); err != nil {
				return fmt.Errorf("stage module ziphash: %w", err)
			}
		}
		info, err := json.Marshal(map[string]string{"Version": version, "Time": "1970-01-01T00:00:00Z"})
		if err == nil && !fileExists(filepath.Join(downloadDir, version+".info")) {
			_ = publishFile(filepath.Join(downloadDir, version+".info"), info)
		}
	}
	if !fileExists(filepath.Join(downloadDir, version+".mod")) {
		modBytes, modErr := zipMember(zipBytes, module+"@"+version+"/go.mod")
		switch modErr {
		case nil:
			if err := publishFile(filepath.Join(downloadDir, version+".mod"), modBytes); err != nil {
				return fmt.Errorf("stage module go.mod: %w", err)
			}
		default:
			// A zip without the root go.mod is not a module zip; fail closed before staging.
			return fmt.Errorf("module zip %s carries no go.mod: %w", escapedModuleDir+"@"+version, ErrNoModules)
		}
	}
	if dirExists(extracted) {
		return nil
	}
	return extractModuleZip(extracted, module, version, zipBytes)
}

// extractModuleZip unpacks the module zip under the escaped module cache directory: every member
// must stay inside "<module>@<version>/", so a archive cannot plant files anywhere else.
func extractModuleZip(target, module, version string, zipBytes []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return fmt.Errorf("open module zip: %w", err)
	}
	prefix := "module@" + version + "/"
	if module != "" {
		prefix = module + "@" + version + "/"
	}
	for _, file := range reader.File {
		if err := extractModuleZipMember(target, prefix, file); err != nil {
			return err
		}
	}
	if err := prepareDir(target); err != nil {
		return fmt.Errorf("module directory: %w", err)
	}
	return nil
}

func extractModuleZipMember(root, prefix string, file *zip.File) error {
	name := file.Name
	if strings.HasSuffix(name, "/") || file.Mode().IsDir() {
		return nil
	}
	if !file.Mode().IsRegular() {
		return fmt.Errorf("module zip member %s is not a regular file", name)
	}
	destination, err := moduleZipDestination(root, prefix, name)
	if err != nil {
		return err
	}
	data, err := readModuleZipFile(name, file)
	if err != nil {
		return err
	}
	if err := prepareDir(filepath.Dir(destination)); err != nil {
		return fmt.Errorf("module member directory: %w", err)
	}
	if err := publishFile(destination, data); err != nil {
		return fmt.Errorf("stage module member %s: %w", name, err)
	}
	return nil
}

func moduleZipDestination(root, prefix, name string) (string, error) {
	rel, ok := strings.CutPrefix(name, prefix)
	if !ok || rel == "" {
		return "", fmt.Errorf("module zip member %s is outside the module", name)
	}
	rel = cleanMemberName(rel)
	if rel == "" || rel == "." || strings.HasPrefix(rel, "..") || strings.Contains(rel, "\\") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("module zip member %s escapes its module", name)
	}
	return filepath.Join(root, filepath.FromSlash(rel)), nil
}

func readModuleZipFile(name string, file *zip.File) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open module member %s: %w", name, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(rc, maxModuleFileBytes+1))
	closeErr := rc.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, fmt.Errorf("read module member %s: %w", name, err)
	}
	if int64(len(data)) > maxModuleFileBytes {
		return nil, fmt.Errorf("module member %s exceeds %d bytes", name, maxModuleFileBytes)
	}
	return data, nil
}

func zipMember(zipBytes []byte, name string) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, fmt.Errorf("open module zip: %w", err)
	}
	for _, file := range reader.File {
		if file.Name != name || file.Mode().IsDir() || strings.HasSuffix(file.Name, "/") {
			continue
		}
		rc, openErr := file.Open()
		if openErr != nil {
			return nil, fmt.Errorf("open module member %s: %w", name, openErr)
		}
		data, readErr := io.ReadAll(io.LimitReader(rc, maxGomodBytes))
		closeErr := rc.Close()
		if readErr != nil || closeErr != nil {
			return nil, fmt.Errorf("read module member %s: %w", name, errors.Join(readErr, closeErr))
		}
		return data, nil
	}
	return nil, fmt.Errorf("module zip carries no %s", name)
}

// hashModuleZip follows the h1: module hash: sha256 over the sorted
// "hex(sha256(content)) two-spaces name newline" lines, base64 encoded, prefixed "h1:".
func hashModuleZip(zipBytes []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return "", fmt.Errorf("open module hash: %w", err)
	}
	summary := sha256.New()
	for _, file := range reader.File {
		hasher := sha256.New()
		rc, openErr := file.Open()
		if openErr != nil {
			return "", fmt.Errorf("open module member %s: %w", file.Name, openErr)
		}
		_, copyErr := io.Copy(hasher, io.LimitReader(rc, maxModuleFileBytes+1))
		closeErr := rc.Close()
		if copyErr != nil || closeErr != nil {
			return "", fmt.Errorf("read module member %s: %w", file.Name, errors.Join(copyErr, closeErr))
		}
		if _, err := fmt.Fprintf(summary, "%x  %s\n", hasher.Sum(nil), file.Name); err != nil {
			return "", fmt.Errorf("write module hash summary: %w", err)
		}
	}
	return "h1:" + base64.StdEncoding.EncodeToString(summary.Sum(nil)), nil
}

// prepareDir creates one directory tree readable by the container user (the jobs mount read-only).
func prepareDir(dir string) error {
	return os.MkdirAll(dir, 0o750)
}

// publishFile atomically publishes cache data with owner-only permissions. The temporary
// file is created beside the destination so rename remains atomic on the cache filesystem.
func publishFile(destination string, data []byte) (resultErr error) {
	if err := prepareDir(filepath.Dir(destination)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(destination), ".heimdall-cache-*")
	if err != nil {
		return fmt.Errorf("create cache temp file: %w", err)
	}
	tmp := f.Name()
	defer func() {
		if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove cache temporary file: %w", err))
		}
	}()
	if err := f.Chmod(0640); err != nil {
		_ = f.Close()
		return fmt.Errorf("secure cache temp file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write cache temp file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync cache temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close cache temp file: %w", err)
	}
	if err := os.Rename(tmp, destination); err != nil {
		return fmt.Errorf("publish cache file: %w", err)
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
