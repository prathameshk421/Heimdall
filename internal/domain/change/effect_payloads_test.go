package change_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

func TestRoomOpenPayloadDecodesStrictly(t *testing.T) {
	p, err := change.DecodeRoomOpenPayload(json.RawMessage(`{"ticket":"KAN-7","invite":["role:test-author","person:ana"]}`))
	require.NoError(t, err)
	require.Equal(t, change.RoomOpenPayload{Ticket: "KAN-7", Invite: []string{"role:test-author", "person:ana"}}, p)
}

func TestRoomOpenPayloadRefusesMissingUnknownAndTrailing(t *testing.T) {
	for name, raw := range map[string]string{
		"missing ticket":  `{"invite":[]}`,
		"missing invite":  `{"ticket":"KAN-7"}`,
		"unknown field":   `{"ticket":"KAN-7","invite":[],"title":"x"}`,
		"trailing data":   `{"ticket":"KAN-7","invite":[]} {}`,
		"null":            `null`,
		"not an object":   `["KAN-7"]`,
		"pr stream":       `{"ticket":"pr-a-b-4","invite":[]}`,
		"placeholder":     `{"ticket":"KAN-7","invite":["person:$assignee"]}`,
		"bad invite":      `{"ticket":"KAN-7","invite":["admin:root"]}`,
		"too many invite": `{"ticket":"KAN-7","invite":[` + strings.Repeat(`"role:x",`, 32) + `"role:x"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := change.DecodeRoomOpenPayload(json.RawMessage(raw))
			require.Error(t, err)
		})
	}
}

func TestBranchPayloadDecodesStrictly(t *testing.T) {
	p, err := change.DecodeBranchPayload(json.RawMessage(`{"repo":"aarush323/heimdall-fixture-pay","ticket":"KAN-7","template":"{ticket}-{slug}"}`))
	require.NoError(t, err)
	require.Equal(t, change.BranchPayload{Repo: "aarush323/heimdall-fixture-pay", Ticket: "KAN-7", Template: "{ticket}-{slug}"}, p)
	require.Equal(t, "KAN-7-fix-refunds", p.Name("fix-refunds"))
}

func TestBranchPayloadRefusesMissingAndBadFields(t *testing.T) {
	for name, raw := range map[string]string{
		"missing repo":     `{"ticket":"KAN-7","template":"{ticket}-{slug}"}`,
		"missing template": `{"repo":"a/b","ticket":"KAN-7"}`,
		"missing ticket":   `{"repo":"a/b","template":"{ticket}-{slug}"}`,
		"unknown field":    `{"repo":"a/b","ticket":"KAN-7","template":"{ticket}-{slug}","x":1}`,
		"bad repo":         `{"repo":"nope","ticket":"KAN-7","template":"{ticket}-{slug}"}`,
		"template no key":  `{"repo":"a/b","ticket":"KAN-7","template":"feature-{slug}"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := change.DecodeBranchPayload(json.RawMessage(raw))
			require.Error(t, err)
		})
	}
}

func TestCheckBranchTemplateAcceptsAndRefuses(t *testing.T) {
	require.NoError(t, change.CheckBranchTemplate("{ticket}-{slug}"))
	require.NoError(t, change.CheckBranchTemplate("{ticket}-wip-{slug}"))
	for _, bad := range []string{"", "{ticket}", "{ticket}{slug}", "{ticket}-a b", "{ticket}-a..b", "{ticket}-{owner}", "{ticket}-" + strings.Repeat("a", 200)} {
		require.Error(t, change.CheckBranchTemplate(bad), bad)
	}
}

func TestCheckInviteTemplateAllowsAssigneePlaceholder(t *testing.T) {
	require.NoError(t, change.CheckInviteTemplate([]string{"role:test-author", "person:$assignee"}))
	require.NoError(t, change.CheckInviteTemplate([]string{}))
	require.Error(t, change.CheckInviteTemplate(nil), "a nil list is refused")
	require.Error(t, change.CheckInviteTemplate([]string{"role:Test Author"}))
	require.Error(t, change.CheckInviteTemplate([]string{"person:$other"}))
}

func TestInviteWithAssigneeReplacesOnlyThePlaceholderAndCopies(t *testing.T) {
	in := []string{"role:test-author", "person:$assignee", "person:bob"}
	out := change.InviteWithAssignee(in, "ana")
	require.Equal(t, []string{"role:test-author", "person:ana", "person:bob"}, out)
	require.Equal(t, "person:$assignee", in[1], "the input list is not changed")
}
