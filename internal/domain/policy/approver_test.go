package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApproverOfFollowsDomainsThenTheDefaultGroup(t *testing.T) {
	p := Policy{
		Approvers: map[string][]string{"default": {"dan"}, "money_movement": {"bob"}},
		Domains:   map[string]string{"internal/pay/**": "money_movement"},
	}
	assert.True(t, p.ApproverOf("bob", "internal/pay/a.go"))
	assert.False(t, p.ApproverOf("dan", "internal/pay/a.go"), "a domain replaces the default group")
	assert.True(t, p.ApproverOf("dan", "web/a.ts"))
	assert.False(t, p.ApproverOf("bob", "web/a.ts"))
	assert.False(t, p.ApproverOf("", "web/a.ts"))
}

func TestAPolicyOwnerIsNotAnApproverByThatAlone(t *testing.T) {
	p := Policy{Approvers: map[string][]string{"default": {"dan"}}, PolicyOwners: []string{"ola"}}
	assert.False(t, p.ApproverOf("ola", "web/a.ts"))
}
