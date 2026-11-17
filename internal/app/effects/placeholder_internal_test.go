package effects

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
)

// Live KAN-25: list_payments_scope_test.go used the helpers of list_payments_test.go, which imports the product. Go
// judges imports per package, so the package reaches the product and must not be refused.
func TestPlaceholderReasonsJudgesEachTestPackage(t *testing.T) {
	product := `package kan25_test

import (
	"testing"

	"github.com/aarush323/heimdall-fixture-pay/internal/app"
)

func newHandler() any { return app.New(app.Config{}) }
`
	sibling := `package kan25_test

import "testing"

func TestScope(t *testing.T) { _ = newHandler() }
`
	stdlibOnly := `package other_test

import "testing"

func TestNothing(t *testing.T) { t.Fatal("not implemented") }
`
	ok := testset.Submission{Files: []testset.File{
		{Path: "tests/heimdall/KAN-25/list_test.go", Content: product},
		{Path: "tests/heimdall/KAN-25/scope_test.go", Content: sibling},
	}}
	require.Empty(t, placeholderReasons(ok), "a sibling of a file that imports the product is fine")

	bad := testset.Submission{Files: []testset.File{{Path: "tests/heimdall/KAN-26/fake_test.go", Content: stdlibOnly}}}
	require.Equal(t, []string{"tests/heimdall/KAN-26 never imports the product, so its tests cannot test it. Call the service through its test seam."},
		placeholderReasons(bad))
}
