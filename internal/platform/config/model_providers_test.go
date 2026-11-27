package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTheTwoShippedProvidersWorkWithoutAnyConfiguration(t *testing.T) {
	t.Parallel()
	cfg, err := Load(writeTOML(t, goodProject))
	require.NoError(t, err)

	deploy, ok := cfg.ModelProvider("deploy")
	require.True(t, ok)
	require.Equal(t, ModelProvider{Name: "deploy", BaseURL: "https://api.deploy.com/api/v1", KeyFile: "deploy_api_key"}, deploy)

	openRouter, ok := cfg.ModelProvider("openrouter")
	require.True(t, ok)
	require.Equal(t, ModelProvider{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1", KeyFile: "openrouter_api_key"}, openRouter)

	// A zero Config (a test double) still knows them.
	_, ok = Config{}.ModelProvider("deploy")
	require.True(t, ok)
}

func TestModelProvidersAreReadFromConfig(t *testing.T) {
	t.Parallel()
	cfg, err := Load(writeTOML(t, goodProject+`
[[model_providers]]
name = "ollama"
base_url = "http://127.0.0.1:11434/v1"

[[model_providers]]
name = "litellm"
base_url = "https://llm.example.test/v1/"
key_file = "litellm_api_key"
headers = { "X-Team" = "platform" }

[[model_providers]]
name = "lan"
base_url = "http://192.168.1.20:8000/v1"
allow_private_http = true
`))
	require.NoError(t, err)

	ollama, ok := cfg.ModelProvider("ollama")
	require.True(t, ok)
	require.Equal(t, ModelProvider{Name: "ollama", BaseURL: "http://127.0.0.1:11434/v1"}, ollama, "no key_file means no key")

	litellm, ok := cfg.ModelProvider("litellm")
	require.True(t, ok)
	require.Equal(t, ModelProvider{
		Name: "litellm", BaseURL: "https://llm.example.test/v1/", KeyFile: "litellm_api_key", Headers: map[string]string{"X-Team": "platform"},
	}, litellm)

	_, ok = cfg.ModelProvider("lan")
	require.True(t, ok)
	_, ok = cfg.ModelProvider("nope")
	require.False(t, ok)

	require.Equal(t, []string{"ollama", "litellm", "lan", "deploy", "openrouter"}, cfg.ModelProviderNames())
}

func TestBadModelProvidersAreRefusedAndNamed(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ toml, want string }{
		"missing name":      {`base_url = "https://x.test/v1"`, "model_providers[0]: name is required"},
		"bad name":          {"name = \"Has/Slash\"\nbase_url = \"https://x.test/v1\"", "model_providers[0]: name"},
		"missing base url":  {`name = "p"`, "model_providers[0].base_url is required"},
		"http public host":  {"name = \"p\"\nbase_url = \"http://llm.example.test/v1\"", "http is allowed only for a loopback host"},
		"http public ip":    {"name = \"p\"\nbase_url = \"http://8.8.8.8/v1\"\nallow_private_http = true", "private address"},
		"http private deny": {"name = \"p\"\nbase_url = \"http://10.0.0.5:8000/v1\"", "allow_private_http"},
		"http hostname":     {"name = \"p\"\nbase_url = \"http://ollama:11434/v1\"\nallow_private_http = true", "private address"},
		"ftp":               {"name = \"p\"\nbase_url = \"ftp://x.test/v1\"", "must use https"},
		"credentials":       {"name = \"p\"\nbase_url = \"https://user:pw@x.test/v1\"", "credentials"},
		"query":             {"name = \"p\"\nbase_url = \"https://x.test/v1?key=abc\"", "query"},
		"key is a url":      {"name = \"p\"\nbase_url = \"https://x.test/v1\"\nkey_file = \"https://x.test\"", "not a URL"},
		"key has a path":    {"name = \"p\"\nbase_url = \"https://x.test/v1\"\nkey_file = \"../k\"", "model_providers[0].key_file"},
		"header authz":      {"name = \"p\"\nbase_url = \"https://x.test/v1\"\nheaders = { \"authorization\" = \"Bearer abc\" }", "Authorization"},
		"header newline":    {"name = \"p\"\nbase_url = \"https://x.test/v1\"\nheaders = { \"X-A\" = \"a\\r\\nX-B: b\" }", "control character"},
		"header bad name":   {"name = \"p\"\nbase_url = \"https://x.test/v1\"\nheaders = { \"X A\" = \"a\" }", "header name"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeTOML(t, goodProject+"\n[[model_providers]]\n"+tc.toml+"\n"))
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestADuplicateProviderNameIsRefused(t *testing.T) {
	t.Parallel()
	_, err := Load(writeTOML(t, goodProject+`
[[model_providers]]
name = "p"
base_url = "https://a.test/v1"

[[model_providers]]
name = "p"
base_url = "https://b.test/v1"
`))
	require.ErrorContains(t, err, `model_providers[1]: name "p" already defined at model_providers[0]`)
}

func TestAPrivateAddressOverHTTPIsAllowedOnlyWhenTheOperatorSaysSo(t *testing.T) {
	t.Parallel()
	for _, base := range []string{"http://10.1.2.3:8000/v1", "http://172.16.0.9/v1", "http://192.168.0.4/v1", "http://[fd00::5]:8000/v1"} {
		_, err := Load(writeTOML(t, goodProject+"\n[[model_providers]]\nname = \"p\"\nbase_url = \""+base+"\"\nallow_private_http = true\n"))
		require.NoError(t, err, base)
	}
	// The flag never makes a public host acceptable, and loopback needs no flag.
	_, err := Load(writeTOML(t, goodProject+"\n[[model_providers]]\nname = \"p\"\nbase_url = \"http://[::1]:8000/v1\"\n"))
	require.NoError(t, err)
}
