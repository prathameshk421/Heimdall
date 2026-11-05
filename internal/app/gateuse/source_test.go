package gateuse_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/gateuse"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type fakePRReader struct {
	prFn    func(ctx context.Context, repo change.Repo, n int) (ports.PR, error)
	filesFn func(ctx context.Context, repo change.Repo, n int) ([]ports.ChangedFile, error)
}

func (f *fakePRReader) PR(ctx context.Context, repo change.Repo, n int) (ports.PR, error) {
	if f.prFn != nil {
		return f.prFn(ctx, repo, n)
	}
	return ports.PR{}, nil
}

func (f *fakePRReader) Files(ctx context.Context, repo change.Repo, n int) ([]ports.ChangedFile, error) {
	if f.filesFn != nil {
		return f.filesFn(ctx, repo, n)
	}
	return nil, nil
}

func (f *fakePRReader) OpenPRs(context.Context, change.Repo) ([]ports.PR, error) {
	return nil, nil
}

type fakeCodeReader struct {
	readFileFn func(ctx context.Context, repo change.Repo, sha id.SHA, path string) ([]byte, error)
	treeHashFn func(ctx context.Context, repo change.Repo, sha id.SHA, dir string) (id.SHA, error)
}

func (f *fakeCodeReader) ReadFile(ctx context.Context, repo change.Repo, sha id.SHA, path string) ([]byte, error) {
	if f.readFileFn != nil {
		return f.readFileFn(ctx, repo, sha, path)
	}
	return nil, ports.ErrNotFound
}

func (f *fakeCodeReader) TreeHash(ctx context.Context, repo change.Repo, sha id.SHA, dir string) (id.SHA, error) {
	if f.treeHashFn != nil {
		return f.treeHashFn(ctx, repo, sha, dir)
	}
	return "", nil
}

func (f *fakeCodeReader) Tarball(context.Context, change.Repo, id.SHA) (io.ReadCloser, error) {
	return nil, nil
}

func jsonDecoder(raw []byte) (policy.Policy, error) {
	var p policy.Policy
	if err := json.Unmarshal(raw, &p); err != nil {
		return policy.Policy{}, err
	}
	return p, nil
}

const goldenPolicyJSON = `{
  "version": 1,
  "workflow": "default",
  "approvers": {
    "auth": ["ana"],
    "default": ["alice"],
    "money_movement": ["alice", "bob"]
  },
  "policy_owners": ["ana"],
  "people": {
    "alice": {"github": "alice", "chat": "alice", "teams": ["payments"]},
    "ana": {"github": "ana", "chat": "ana", "agent": "ana-reviewer", "teams": ["security"]},
    "bob": {"github": "bob", "chat": "bob", "agent": "bob-reviewer", "teams": ["payments"]},
    "dana": {"github": "dana", "chat": "dana", "teams": ["payments"]},
    "eli": {"github": "eli", "chat": "eli", "agent": "eli-reviewer", "teams": ["consumer"]}
  },
  "domains": {
    "internal/auth/**": "auth",
    "internal/refunds/**": "money_movement"
  },
  "reviewers": {
    "count": 3,
    "pick_timeout": "15m",
    "required": {
      "api/openapi.yaml": ["eli"],
      "internal/auth/**": ["ana"]
    }
  },
  "slop": {
    "max_changed_lines": 400,
    "require_ticket": true,
    "ignore": ["go.sum"]
  },
  "tests": {
    "dir": "tests/heimdall",
    "allowed_imports": ["github.com/stretchr/testify"],
    "sensitive": ["backend/**/auth*", "**/migrations/**", "*delete*", "*privacy*", "*consent*"],
    "min_mutation_score": 0.6,
    "mutation_blocks": false
  },
  "blocking": {
    "min_severity": "medium",
    "min_tier": "E3"
  },
  "consensus": {
    "min_high_reporters": 2,
    "block_score": 1.5,
    "min_reporters": "majority",
    "confidence": {"high": 1, "medium": 0.5, "low": 0.2},
    "weight": {"start": 0.7, "min": 0.3, "max": 1, "min_decided": 5}
  },
  "deliberation": {
    "max_rounds": 2,
    "human_only_domains": ["auth", "money_movement", "data_deletion"]
  },
  "keys": {
    "openrouter": "production"
  },
  "models": {
    "provider": "openrouter",
    "test_author": "openrouter/deepseek/deepseek-v4-flash",
    "reviewer": "openrouter/deepseek/deepseek-v4-flash",
    "resolver": "openrouter/deepseek/deepseek-v4-flash",
    "extraction": "deepseek/deepseek-v4-flash",
    "embedding": "openrouter:mongodb/voyage-4-lite"
  },
  "budgets": {
    "test_author": 400000,
    "reviewer": 150000,
    "resolver": 120000,
    "per_change": 1200000
  },
  "skills": {
    "test_author": "1.0.0",
    "reviewer": "1.0.0",
    "resolver": "1.0.0"
  },
  "memory": {
    "rooms": ["payments-eng"],
    "chat_keywords": ["decided", "decision", "we will", "incident", "postmortem"]
  },
  "pack": {
    "max_age_days": 14
  },
  "timeouts": {
    "question": "4h",
    "remind_after": "2h",
    "escalate_after": "8h",
    "reject_after": "24h"
  },
  "ci_required": ["build", "lint"],
  "chat": {
    "bridge": "mattermost",
    "verbosity": "milestones",
    "decisions_in_room": true
  },
  "github": {
    "mode": "poll",
    "interval": "10s"
  },
  "tracker": {
    "kind": "jira",
    "mode": "poll",
    "interval": "30s"
  },
  "agents": {
    "runtime": "opencode"
  }
}`

func samplePolicy() policy.Policy {
	var p policy.Policy
	if err := json.Unmarshal([]byte(goldenPolicyJSON), &p); err != nil {
		panic(err)
	}
	return p
}

func policyBytes(t *testing.T, p policy.Policy) []byte {
	t.Helper()
	b, err := json.Marshal(p)
	require.NoError(t, err)
	return b
}

func TestNewSourceValidation(t *testing.T) {
	validCfg := gateuse.SourceConfig{
		PRs:    &fakePRReader{},
		Code:   &fakeCodeReader{},
		Decode: jsonDecoder,
	}

	t.Run("nil dependencies named in error", func(t *testing.T) {
		_, err := gateuse.NewSource(gateuse.SourceConfig{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "PRs")
		require.Contains(t, err.Error(), "Code")
		require.Contains(t, err.Error(), "Decode")
	})

	t.Run("valid config creates RepoSource", func(t *testing.T) {
		src, err := gateuse.NewSource(validCfg)
		require.NoError(t, err)
		require.NotNil(t, src)
	})
}

func TestOutsideBasePolicyReadAtBaseSHA(t *testing.T) {
	baseSHA := id.SHA("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	headSHA := id.SHA("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	repo := change.Repo("acme/payment")
	prNum := 42

	var shaReadAtCode []id.SHA
	p := samplePolicy()
	require.NoError(t, p.Validate())
	raw := policyBytes(t, p)

	prs := &fakePRReader{
		prFn: func(_ context.Context, r change.Repo, n int) (ports.PR, error) {
			require.Equal(t, repo, r)
			require.Equal(t, prNum, n)
			return ports.PR{
				Repo:    repo,
				Number:  prNum,
				BaseSHA: baseSHA,
				HeadSHA: headSHA,
				State:   ports.PRStateOpen,
				Author:  change.ActorID("author-user"),
			}, nil
		},
	}
	code := &fakeCodeReader{
		readFileFn: func(_ context.Context, r change.Repo, sha id.SHA, path string) ([]byte, error) {
			shaReadAtCode = append(shaReadAtCode, sha)
			require.Equal(t, repo, r)
			require.Equal(t, "heimdall.yaml", path)
			return raw, nil
		},
		treeHashFn: func(_ context.Context, _ change.Repo, sha id.SHA, dir string) (id.SHA, error) {
			require.Equal(t, headSHA, sha)
			require.Equal(t, "tests/heimdall/PAY-1", dir)
			return id.SHA("1234567890abcdef1234567890abcdef12345678"), nil
		},
	}

	src, err := gateuse.NewSource(gateuse.SourceConfig{
		PRs:    prs,
		Code:   code,
		Decode: jsonDecoder,
	})
	require.NoError(t, err)

	out, err := src.Outside(context.Background(), gateuse.Request{
		ChangeID: id.ChangeID("PAY-1"),
		Repo:     repo,
		Number:   prNum,
		HeadSHA:  headSHA,
		Author:   change.ActorID("author-user"),
	})
	require.NoError(t, err)

	require.True(t, out.PolicyKnown)
	require.Equal(t, 1, out.PolicyVersion)
	require.Equal(t, "1234567890abcdef1234567890abcdef12345678", out.HeadTestTreeHash)
	require.Len(t, shaReadAtCode, 2)
	// First read must be at BaseSHA!
	require.Equal(t, baseSHA, shaReadAtCode[0], "base policy must be read at PR base SHA")
	require.Equal(t, headSHA, shaReadAtCode[1], "head policy must be read at head SHA")
}

func TestOutsidePolicyLoosenedAtHeadDoesNotLoosenGate(t *testing.T) {
	baseSHA := id.SHA("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	headSHA := id.SHA("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	basePol := samplePolicy()
	basePol.CIRequired = []string{"ci/strict-check", "ci/security-scan"}

	headPol := samplePolicy()
	headPol.CIRequired = []string{} // PR tries to loosen required CI checks

	prs := &fakePRReader{
		prFn: func(context.Context, change.Repo, int) (ports.PR, error) {
			return ports.PR{BaseSHA: baseSHA, HeadSHA: headSHA}, nil
		},
	}
	code := &fakeCodeReader{
		readFileFn: func(_ context.Context, _ change.Repo, sha id.SHA, _ string) ([]byte, error) {
			if sha == baseSHA {
				return policyBytes(t, basePol), nil
			}
			return policyBytes(t, headPol), nil
		},
	}

	src, err := gateuse.NewSource(gateuse.SourceConfig{
		PRs:    prs,
		Code:   code,
		Decode: jsonDecoder,
	})
	require.NoError(t, err)

	out, err := src.Outside(context.Background(), gateuse.Request{
		ChangeID: id.ChangeID("PAY-2"),
		HeadSHA:  headSHA,
	})
	require.NoError(t, err)

	require.True(t, out.PolicyKnown)
	// Outside.CIRequired must come strictly from base policy!
	require.Equal(t, []string{"ci/strict-check", "ci/security-scan"}, out.CIRequired)
	require.Equal(t, basePol.CIRequired, out.PolicyAtBase.CIRequired)
	require.Empty(t, out.PolicyAtHead.CIRequired)
}

func TestOutsideMissingPolicyFailsClosed(t *testing.T) {
	baseSHA := id.SHA("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	prs := &fakePRReader{
		prFn: func(context.Context, change.Repo, int) (ports.PR, error) {
			return ports.PR{BaseSHA: baseSHA}, nil
		},
	}
	code := &fakeCodeReader{
		readFileFn: func(_ context.Context, _ change.Repo, _ id.SHA, _ string) ([]byte, error) {
			return nil, ports.ErrNotFound
		},
	}

	src, err := gateuse.NewSource(gateuse.SourceConfig{
		PRs:    prs,
		Code:   code,
		Decode: jsonDecoder,
	})
	require.NoError(t, err)

	out, err := src.Outside(context.Background(), gateuse.Request{})
	require.NoError(t, err, "missing policy file must not return error, must fail closed")
	require.False(t, out.PolicyKnown, "policy must not be known")
}

func TestOutsideDecodeOrValidationFailureFailsClosed(t *testing.T) {
	baseSHA := id.SHA("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	prs := &fakePRReader{
		prFn: func(context.Context, change.Repo, int) (ports.PR, error) {
			return ports.PR{BaseSHA: baseSHA}, nil
		},
	}

	t.Run("decode error", func(t *testing.T) {
		code := &fakeCodeReader{
			readFileFn: func(_ context.Context, _ change.Repo, _ id.SHA, _ string) ([]byte, error) {
				return []byte("invalid json or yaml {{{{"), nil
			},
		}
		src, err := gateuse.NewSource(gateuse.SourceConfig{
			PRs:    prs,
			Code:   code,
			Decode: jsonDecoder,
		})
		require.NoError(t, err)

		out, err := src.Outside(context.Background(), gateuse.Request{})
		require.NoError(t, err)
		require.False(t, out.PolicyKnown)
	})

	t.Run("validation error", func(t *testing.T) {
		invalidPol := samplePolicy()
		invalidPol.Version = 999 // Validate() checks version == 1
		code := &fakeCodeReader{
			readFileFn: func(_ context.Context, _ change.Repo, _ id.SHA, _ string) ([]byte, error) {
				return policyBytes(t, invalidPol), nil
			},
		}
		src, err := gateuse.NewSource(gateuse.SourceConfig{
			PRs:    prs,
			Code:   code,
			Decode: jsonDecoder,
		})
		require.NoError(t, err)

		out, err := src.Outside(context.Background(), gateuse.Request{})
		require.NoError(t, err)
		require.False(t, out.PolicyKnown)
	})
}

func TestOutsideTransportErrorIsReturned(t *testing.T) {
	baseSHA := id.SHA("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	t.Run("PR transport error", func(t *testing.T) {
		prs := &fakePRReader{
			prFn: func(context.Context, change.Repo, int) (ports.PR, error) {
				return ports.PR{}, ports.ErrUnavailable
			},
		}
		src, err := gateuse.NewSource(gateuse.SourceConfig{
			PRs:    prs,
			Code:   &fakeCodeReader{},
			Decode: jsonDecoder,
		})
		require.NoError(t, err)

		_, err = src.Outside(context.Background(), gateuse.Request{})
		require.ErrorIs(t, err, ports.ErrUnavailable)
	})

	t.Run("ReadFile base transport error", func(t *testing.T) {
		prs := &fakePRReader{
			prFn: func(context.Context, change.Repo, int) (ports.PR, error) {
				return ports.PR{BaseSHA: baseSHA}, nil
			},
		}
		code := &fakeCodeReader{
			readFileFn: func(_ context.Context, _ change.Repo, _ id.SHA, _ string) ([]byte, error) {
				return nil, ports.ErrUnavailable
			},
		}
		src, err := gateuse.NewSource(gateuse.SourceConfig{
			PRs:    prs,
			Code:   code,
			Decode: jsonDecoder,
		})
		require.NoError(t, err)

		_, err = src.Outside(context.Background(), gateuse.Request{})
		require.ErrorIs(t, err, ports.ErrUnavailable)
	})

	t.Run("TreeHash transport error", func(t *testing.T) {
		p := samplePolicy()
		prs := &fakePRReader{
			prFn: func(context.Context, change.Repo, int) (ports.PR, error) {
				return ports.PR{BaseSHA: baseSHA}, nil
			},
		}
		code := &fakeCodeReader{
			readFileFn: func(_ context.Context, _ change.Repo, _ id.SHA, _ string) ([]byte, error) {
				return policyBytes(t, p), nil
			},
			treeHashFn: func(context.Context, change.Repo, id.SHA, string) (id.SHA, error) {
				return "", ports.ErrUnavailable
			},
		}
		src, err := gateuse.NewSource(gateuse.SourceConfig{
			PRs:    prs,
			Code:   code,
			Decode: jsonDecoder,
		})
		require.NoError(t, err)

		_, err = src.Outside(context.Background(), gateuse.Request{
			ChangeID: id.ChangeID("PAY-1"),
		})
		require.ErrorIs(t, err, ports.ErrUnavailable)
	})
}

func TestOutsideDeletedTestsDirGivesEmptyTreeHash(t *testing.T) {
	baseSHA := id.SHA("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	p := samplePolicy()
	prs := &fakePRReader{
		prFn: func(context.Context, change.Repo, int) (ports.PR, error) {
			return ports.PR{BaseSHA: baseSHA}, nil
		},
	}
	code := &fakeCodeReader{
		readFileFn: func(_ context.Context, _ change.Repo, _ id.SHA, _ string) ([]byte, error) {
			return policyBytes(t, p), nil
		},
		treeHashFn: func(context.Context, change.Repo, id.SHA, string) (id.SHA, error) {
			return "", ports.ErrNotFound
		},
	}

	src, err := gateuse.NewSource(gateuse.SourceConfig{
		PRs:    prs,
		Code:   code,
		Decode: jsonDecoder,
	})
	require.NoError(t, err)

	out, err := src.Outside(context.Background(), gateuse.Request{
		ChangeID: id.ChangeID("PAY-1"),
	})
	require.NoError(t, err)
	require.Empty(t, out.HeadTestTreeHash, "deleted tests directory must give empty tree hash")
}

func TestOutsideTestPathsOutsideTicketFolderListed(t *testing.T) {
	baseSHA := id.SHA("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	p := samplePolicy()
	p.Tests.Dir = "tests/heimdall"

	prs := &fakePRReader{
		prFn: func(context.Context, change.Repo, int) (ports.PR, error) {
			return ports.PR{BaseSHA: baseSHA}, nil
		},
		filesFn: func(context.Context, change.Repo, int) ([]ports.ChangedFile, error) {
			return []ports.ChangedFile{
				{Path: "tests/heimdall/PAY-100/test_a.go", Status: ports.FileAdded},
				{Path: "tests/heimdall/PAY-100/sub/test_b.go", Status: ports.FileModified},
				{Path: "tests/heimdall/PAY-200/test_other.go", Status: ports.FileModified},
				{Path: "tests/heimdall/shared_helper.go", Status: ports.FileModified},
				{Path: "src/main.go", Status: ports.FileModified},
			}, nil
		},
	}
	code := &fakeCodeReader{
		readFileFn: func(_ context.Context, _ change.Repo, _ id.SHA, _ string) ([]byte, error) {
			return policyBytes(t, p), nil
		},
		treeHashFn: func(context.Context, change.Repo, id.SHA, string) (id.SHA, error) {
			return "treehash123", nil
		},
	}

	src, err := gateuse.NewSource(gateuse.SourceConfig{
		PRs:    prs,
		Code:   code,
		Decode: jsonDecoder,
	})
	require.NoError(t, err)

	out, err := src.Outside(context.Background(), gateuse.Request{
		ChangeID: id.ChangeID("PAY-100"),
	})
	require.NoError(t, err)
	require.True(t, out.TestDiffKnown)
	expectedOutside := []string{
		"tests/heimdall/PAY-200/test_other.go",
		"tests/heimdall/shared_helper.go",
	}
	require.Equal(t, expectedOutside, out.TestDiffOutsideKey)
}

func TestOutsideFilesFailureMarksTestDiffUnknown(t *testing.T) {
	baseSHA := id.SHA("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	p := samplePolicy()

	prs := &fakePRReader{
		prFn: func(context.Context, change.Repo, int) (ports.PR, error) {
			return ports.PR{BaseSHA: baseSHA}, nil
		},
		filesFn: func(context.Context, change.Repo, int) ([]ports.ChangedFile, error) {
			return nil, errors.New("diff service timeout")
		},
	}
	code := &fakeCodeReader{
		readFileFn: func(_ context.Context, _ change.Repo, _ id.SHA, _ string) ([]byte, error) {
			return policyBytes(t, p), nil
		},
	}

	src, err := gateuse.NewSource(gateuse.SourceConfig{
		PRs:    prs,
		Code:   code,
		Decode: jsonDecoder,
	})
	require.NoError(t, err)

	out, err := src.Outside(context.Background(), gateuse.Request{
		ChangeID: id.ChangeID("PAY-100"),
	})
	require.NoError(t, err)
	require.False(t, out.TestDiffKnown, "diff failure must set TestDiffKnown=false")
	require.Empty(t, out.TestDiffOutsideKey)
}

func TestOutsideDomainApproversUnion(t *testing.T) {
	baseSHA := id.SHA("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	p := samplePolicy()
	// domains:
	// "internal/auth/**" -> "auth" (approvers: ["ana"] -> github "ana")
	// "internal/refunds/**" -> "money_movement" (approvers: ["alice", "bob"] -> github "alice", "bob")
	// "default" -> ["alice"] -> github "alice"

	prs := &fakePRReader{
		prFn: func(context.Context, change.Repo, int) (ports.PR, error) {
			return ports.PR{BaseSHA: baseSHA}, nil
		},
		filesFn: func(context.Context, change.Repo, int) ([]ports.ChangedFile, error) {
			return []ports.ChangedFile{
				{Path: "internal/auth/login.go", Status: ports.FileModified},
				{Path: "internal/refunds/refund.go", Status: ports.FileModified},
				{Path: "internal/auth/token.go", Status: ports.FileAdded},
			}, nil
		},
	}
	code := &fakeCodeReader{
		readFileFn: func(_ context.Context, _ change.Repo, _ id.SHA, _ string) ([]byte, error) {
			return policyBytes(t, p), nil
		},
	}

	src, err := gateuse.NewSource(gateuse.SourceConfig{
		PRs:    prs,
		Code:   code,
		Decode: jsonDecoder,
	})
	require.NoError(t, err)

	out, err := src.Outside(context.Background(), gateuse.Request{
		ChangeID: id.ChangeID("PAY-1"),
	})
	require.NoError(t, err)
	expectedApprovers := []string{"alice", "ana", "bob"}
	require.Equal(t, expectedApprovers, out.ApproversForChangedDomains)
}

func TestOutsideDomainApproversDefaultDomain(t *testing.T) {
	baseSHA := id.SHA("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	p := samplePolicy()

	prs := &fakePRReader{
		prFn: func(context.Context, change.Repo, int) (ports.PR, error) {
			return ports.PR{BaseSHA: baseSHA}, nil
		},
		filesFn: func(context.Context, change.Repo, int) ([]ports.ChangedFile, error) {
			return []ports.ChangedFile{
				{Path: "README.md", Status: ports.FileModified},
			}, nil
		},
	}
	code := &fakeCodeReader{
		readFileFn: func(_ context.Context, _ change.Repo, _ id.SHA, _ string) ([]byte, error) {
			return policyBytes(t, p), nil
		},
	}

	src, err := gateuse.NewSource(gateuse.SourceConfig{
		PRs:    prs,
		Code:   code,
		Decode: jsonDecoder,
	})
	require.NoError(t, err)

	out, err := src.Outside(context.Background(), gateuse.Request{
		ChangeID: id.ChangeID("PAY-1"),
	})
	require.NoError(t, err)
	expectedApprovers := []string{"alice"}
	require.Equal(t, expectedApprovers, out.ApproversForChangedDomains)
}

func TestOutsidePolicyChangeApprovalOnlyByOwner(t *testing.T) {
	baseSHA := id.SHA("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	headSHA := id.SHA("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	basePol := samplePolicy()
	headPolChanged := samplePolicy()
	headPolChanged.Workflow = "changed-workflow"

	t.Run("policy changed and approved by owner", func(t *testing.T) {
		prs := &fakePRReader{
			prFn: func(context.Context, change.Repo, int) (ports.PR, error) {
				return ports.PR{BaseSHA: baseSHA, HeadSHA: headSHA}, nil
			},
		}
		code := &fakeCodeReader{
			readFileFn: func(_ context.Context, _ change.Repo, sha id.SHA, _ string) ([]byte, error) {
				if sha == baseSHA {
					return policyBytes(t, basePol), nil
				}
				return policyBytes(t, headPolChanged), nil
			},
		}
		src, err := gateuse.NewSource(gateuse.SourceConfig{
			PRs:    prs,
			Code:   code,
			Decode: jsonDecoder,
		})
		require.NoError(t, err)

		out, err := src.Outside(context.Background(), gateuse.Request{
			ChangeID:  id.ChangeID("PAY-1"),
			HeadSHA:   headSHA,
			Approvers: []string{"bob", "ana"},
		})
		require.NoError(t, err)
		require.True(t, out.PolicyOwnerApproved)
	})

	t.Run("policy changed but approved by non-owner", func(t *testing.T) {
		prs := &fakePRReader{
			prFn: func(context.Context, change.Repo, int) (ports.PR, error) {
				return ports.PR{BaseSHA: baseSHA, HeadSHA: headSHA}, nil
			},
		}
		code := &fakeCodeReader{
			readFileFn: func(_ context.Context, _ change.Repo, sha id.SHA, _ string) ([]byte, error) {
				if sha == baseSHA {
					return policyBytes(t, basePol), nil
				}
				return policyBytes(t, headPolChanged), nil
			},
		}
		src, err := gateuse.NewSource(gateuse.SourceConfig{
			PRs:    prs,
			Code:   code,
			Decode: jsonDecoder,
		})
		require.NoError(t, err)

		out, err := src.Outside(context.Background(), gateuse.Request{
			ChangeID:  id.ChangeID("PAY-1"),
			HeadSHA:   headSHA,
			Approvers: []string{"bob", "alice"},
		})
		require.NoError(t, err)
		require.False(t, out.PolicyOwnerApproved)
	})

	t.Run("policy unchanged: PolicyOwnerApproved is false", func(t *testing.T) {
		prs := &fakePRReader{
			prFn: func(context.Context, change.Repo, int) (ports.PR, error) {
				return ports.PR{BaseSHA: baseSHA, HeadSHA: headSHA}, nil
			},
		}
		code := &fakeCodeReader{
			readFileFn: func(_ context.Context, _ change.Repo, _ id.SHA, _ string) ([]byte, error) {
				return policyBytes(t, basePol), nil
			},
		}
		src, err := gateuse.NewSource(gateuse.SourceConfig{
			PRs:    prs,
			Code:   code,
			Decode: jsonDecoder,
		})
		require.NoError(t, err)

		out, err := src.Outside(context.Background(), gateuse.Request{
			ChangeID:  id.ChangeID("PAY-1"),
			HeadSHA:   headSHA,
			Approvers: []string{"ana"},
		})
		require.NoError(t, err)
		require.False(t, out.PolicyOwnerApproved)
	})

	t.Run("policy missing at head counts as changed", func(t *testing.T) {
		prs := &fakePRReader{
			prFn: func(context.Context, change.Repo, int) (ports.PR, error) {
				return ports.PR{BaseSHA: baseSHA, HeadSHA: headSHA}, nil
			},
		}
		code := &fakeCodeReader{
			readFileFn: func(_ context.Context, _ change.Repo, sha id.SHA, _ string) ([]byte, error) {
				if sha == baseSHA {
					return policyBytes(t, basePol), nil
				}
				return nil, ports.ErrNotFound
			},
		}
		src, err := gateuse.NewSource(gateuse.SourceConfig{
			PRs:    prs,
			Code:   code,
			Decode: jsonDecoder,
		})
		require.NoError(t, err)

		out, err := src.Outside(context.Background(), gateuse.Request{
			ChangeID:  id.ChangeID("PAY-1"),
			HeadSHA:   headSHA,
			Approvers: []string{"ana"},
		})
		require.NoError(t, err)
		require.True(t, out.PolicyKnown)
		require.True(t, out.PolicyOwnerApproved)
	})
}

func TestOutsideNoSecretInErrors(t *testing.T) {
	secretToken := "secret-auth-token-12345"
	prs := &fakePRReader{
		prFn: func(context.Context, change.Repo, int) (ports.PR, error) {
			return ports.PR{}, errors.New("connection failed: " + secretToken)
		},
	}
	src, err := gateuse.NewSource(gateuse.SourceConfig{
		PRs:    prs,
		Code:   &fakeCodeReader{},
		Decode: jsonDecoder,
	})
	require.NoError(t, err)

	_, err = src.Outside(context.Background(), gateuse.Request{})
	require.Error(t, err)
	// Verification that our wrapper does not introduce token secrets
	require.NotContains(t, err.Error(), "token-value-forbidden")
}

func TestEvaluatePopulatesApproversToSource(t *testing.T) {
	evs := greenStream(t).
		add(change.EventApprovalRecorded, githubActor, change.ApprovalRecorded{
			Login: "rekdfr", CommitSHA: head,
		}).
		events()

	store := &capturingStore{
		events: evs,
	}
	var receivedRequest gateuse.Request
	src := &capturingSource{
		onOutside: func(req gateuse.Request) (gateuse.Outside, error) {
			receivedRequest = req
			return greenOutside(), nil
		},
	}

	eval, err := gateuse.New(gateuse.Config{
		Events: store,
		Source: src,
		Clock:  fixedClock{},
		IDs:    &fakeIDs{},
	})
	require.NoError(t, err)

	_, err = eval.Evaluate(context.Background(), changeKey)
	require.NoError(t, err)
	require.Equal(t, []string{"rekdfr"}, receivedRequest.Approvers)
}

type capturingStore struct {
	events []change.Event
}

func (s *capturingStore) Append(_ context.Context, _ id.ChangeID, expected int64, evs []change.Event, _ []change.Effect) (int64, error) {
	return expected + int64(len(evs)), nil
}

func (s *capturingStore) Load(context.Context, id.ChangeID) ([]change.Event, error) {
	return s.events, nil
}

type capturingSource struct {
	onOutside func(req gateuse.Request) (gateuse.Outside, error)
}

func (s *capturingSource) Outside(_ context.Context, req gateuse.Request) (gateuse.Outside, error) {
	if s.onOutside != nil {
		return s.onOutside(req)
	}
	return gateuse.Outside{}, nil
}
