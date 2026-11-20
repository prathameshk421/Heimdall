package config

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// modelProviderNamePattern is the shape of a provider name. The name is the prefix of an agent model id
// (`<name>/<model>`), so it holds no slash.
var modelProviderNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// headerNamePattern is an HTTP header field name (RFC 9110 token).
var headerNamePattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

// isReservedHeader reports whether the client sets name itself. A configured header must not replace it.
func isReservedHeader(name string) bool {
	return slices.Contains([]string{"authorization", "content-type", "content-length", "host"}, strings.ToLower(name))
}

// ModelProvider is one OpenAI-compatible model endpoint. Name is the prefix of an agent model id. BaseURL is the API
// root that holds /chat/completions. KeyFile is the secret file name of the bearer key; empty means the endpoint
// takes no key (a local server). Headers are extra, non-secret request headers.
type ModelProvider struct {
	Name    string
	BaseURL string
	KeyFile string
	Headers map[string]string
}

// defaultModelProviders are the two providers a deploy already uses. A configured provider of the same name
// replaces them.
func defaultModelProviders() []ModelProvider {
	return []ModelProvider{
		{Name: "deploy", BaseURL: "https://api.deploy.com/api/v1", KeyFile: "deploy_api_key"},
		{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1", KeyFile: "openrouter_api_key"},
	}
}

// fileModelProvider is one [[model_providers]] entry.
type fileModelProvider struct {
	Name             *string           `toml:"name"`
	BaseURL          *string           `toml:"base_url"`
	KeyFile          *string           `toml:"key_file"`
	AllowPrivateHTTP bool              `toml:"allow_private_http"`
	Headers          map[string]string `toml:"headers"`
}

// ModelProvider returns the provider called name: a configured one, else one of the two shipped defaults.
func (c Config) ModelProvider(name string) (ModelProvider, bool) {
	for _, p := range c.ModelProviders {
		if p.Name == name {
			return p, true
		}
	}
	for _, p := range defaultModelProviders() {
		if p.Name == name {
			return p, true
		}
	}
	return ModelProvider{}, false
}

// ModelProviderNames lists every provider name ModelProvider resolves: the configured ones in file order, then the
// shipped defaults that no configured provider replaces.
func (c Config) ModelProviderNames() []string {
	names := make([]string, 0, len(c.ModelProviders)+len(defaultModelProviders()))
	for _, p := range c.ModelProviders {
		names = append(names, p.Name)
	}
	for _, p := range defaultModelProviders() {
		if !slices.Contains(names, p.Name) {
			names = append(names, p.Name)
		}
	}
	return names
}

// addModelProviders validates [[model_providers]] and records the valid entries.
func addModelProviders(cfg *Config, raw []fileModelProvider) []error {
	var errs []error
	firstIndex := map[string]int{}
	for i, r := range raw {
		where := fmt.Sprintf("model_providers[%d]", i)
		p, perrs := checkModelProvider(where, r)
		errs = append(errs, perrs...)
		if len(perrs) > 0 {
			continue
		}
		if first, dup := firstIndex[p.Name]; dup {
			errs = append(errs, fmt.Errorf("%s: name %q already defined at model_providers[%d]", where, p.Name, first))
			continue
		}
		firstIndex[p.Name] = i
		cfg.ModelProviders = append(cfg.ModelProviders, p)
	}
	return errs
}

func checkModelProvider(where string, raw fileModelProvider) (ModelProvider, []error) {
	var errs []error
	switch {
	case raw.Name == nil || *raw.Name == "":
		errs = append(errs, fmt.Errorf("%s: name is required", where))
	case !modelProviderNamePattern.MatchString(*raw.Name):
		errs = append(errs, fmt.Errorf("%s: name %q must match %s", where, *raw.Name, modelProviderNamePattern))
	}
	if err := checkModelProviderURL(where+".base_url", raw.BaseURL, raw.AllowPrivateHTTP); err != nil {
		errs = append(errs, err)
	}
	keyFile := ""
	if raw.KeyFile != nil && *raw.KeyFile != "" {
		if err := checkSecretKey(where+".key_file", raw.KeyFile); err != nil {
			errs = append(errs, err)
		}
		keyFile = *raw.KeyFile
	}
	errs = append(errs, checkModelProviderHeaders(where+".headers", raw.Headers)...)
	if len(errs) > 0 {
		return ModelProvider{}, errs
	}
	return ModelProvider{Name: *raw.Name, BaseURL: *raw.BaseURL, KeyFile: keyFile, Headers: maps.Clone(raw.Headers)}, nil
}

// checkModelProviderURL requires an absolute https URL. Plain http is accepted for a loopback host, and for a private
// IP address only when the operator sets allow_private_http. A hostname other than localhost cannot be judged at
// load time, so it needs https. The URL holds no credentials, query or fragment: a key belongs in key_file.
func checkModelProviderURL(field string, raw *string, allowPrivateHTTP bool) error {
	if raw == nil || *raw == "" {
		return fmt.Errorf("%s is required", field)
	}
	u, err := url.Parse(*raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s %q must be an absolute http(s) URL", field, *raw)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%s must not hold credentials, a query or a fragment; put the key in key_file", field)
	}
	return checkModelProviderScheme(field, u, allowPrivateHTTP)
}

func checkModelProviderScheme(field string, u *url.URL, allowPrivateHTTP bool) error {
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Hostname()) || (allowPrivateHTTP && isPrivateIP(u.Hostname())) {
			return nil
		}
		return fmt.Errorf("%s %q: http is allowed only for a loopback host, or for a private address when allow_private_http = true; use https", field, u.Redacted())
	default:
		return fmt.Errorf("%s %q must use https (or http on a loopback host)", field, u.Redacted())
	}
}

// isPrivateIP reports whether host is an IP literal in a private range (RFC 1918 or fc00::/7).
func isPrivateIP(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsPrivate()
}

func checkModelProviderHeaders(field string, headers map[string]string) []error {
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(headers)) {
		switch {
		case !headerNamePattern.MatchString(name):
			errs = append(errs, fmt.Errorf("%s: header name %q is not a valid HTTP header name", field, name))
		case isReservedHeader(name):
			errs = append(errs, fmt.Errorf("%s: header %q is set by Heimdall; key_file sets the Authorization header", field, name))
		case strings.ContainsFunc(headers[name], func(r rune) bool { return (r < 0x20 && r != '\t') || r == 0x7f }):
			errs = append(errs, errors.New(field+": header "+name+" has a control character in its value"))
		}
	}
	return errs
}
