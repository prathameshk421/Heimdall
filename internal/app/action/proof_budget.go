package action

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// ErrProofBudget means the next model call could pass the run's per-PR cap, so the call was not made. The run then ends
// as unknown for the reason budget (ACTION_MODE 7): a proof run never starts a call it cannot pay for.
var ErrProofBudget = errors.New("action: the next model call could pass the per-run budget")

// framingTokens is an allowance for the tokens a provider adds around the messages and the tool schemas.
const framingTokens = 1000

// ModelPrice is a model's price in USD per million tokens. The proof run bills with the provider's usage, and sizes a
// call before it with CallBound, so the cap can be checked before the spend exists.
type ModelPrice struct {
	InputPerM       float64
	CachedInputPerM float64
	OutputPerM      float64
}

// valid reports whether the price can bound a call. A price of zero would make every call look free.
func (p ModelPrice) valid() bool {
	return p.InputPerM > 0 && p.OutputPerM > 0 && p.CachedInputPerM >= 0
}

// CallBound is an upper bound on the cost of one answer, the same rule the bench uses (bench.Price.CallBound): a
// request of n bytes holds at most n tokens because a token covers at least one byte, the answer holds at most
// maxOutput tokens, and every input token is billed at the full input price, which is never below the cache-read price.
func (p ModelPrice) CallBound(requestBytes, maxOutput int) float64 {
	return (float64(requestBytes+framingTokens)*p.InputPerM + float64(maxOutput)*p.OutputPerM) / 1e6
}

// runBudget holds the per-run caps and the spend so far. It is the one place the proof run decides whether a model call
// may happen, so no call starts that could pass a cap.
type runBudget struct {
	perPRUSD  float64
	inputCap  int64
	outputCap int64
	price     ModelPrice
	spent     proof.Spend
}

// newRunBudget reads the caps of the action area, which Effective has already filled with the defaults.
func newRunBudget(b policy.ActionBudget, price ModelPrice) *runBudget {
	return &runBudget{perPRUSD: b.PerPRUSD, inputCap: int64(b.PerPRTokens.Input),
		outputCap: int64(b.PerPRTokens.Output), price: price}
}

// allow refuses a call whose worst case could pass a cap. The worst case is the request's own size plus the full output
// cap, so a call that is refused is one the run could not afford whatever the model answered.
func (b *runBudget) allow(requestBytes, maxOutput int) error {
	bound := b.price.CallBound(requestBytes, maxOutput)
	if b.spent.USD+bound > b.perPRUSD {
		return fmt.Errorf("%w: spent $%.4f of $%.2f, the next call could cost $%.4f",
			ErrProofBudget, b.spent.USD, b.perPRUSD, bound)
	}
	input := int64(requestBytes + framingTokens)
	if int64(b.spent.InputTokens)+input > b.inputCap {
		return fmt.Errorf("%w: sent %d of %d input tokens, the next call could send %d",
			ErrProofBudget, b.spent.InputTokens, b.inputCap, input)
	}
	if int64(b.spent.OutputTokens)+int64(maxOutput) > b.outputCap {
		return fmt.Errorf("%w: received %d of %d output tokens, the next answer could hold %d",
			ErrProofBudget, b.spent.OutputTokens, b.outputCap, maxOutput)
	}
	return nil
}

// add records what one answer cost. A cost or token count that cannot be true stops the run: a missing measure must
// never look like a cheap one.
func (b *runBudget) add(resp ports.ModelResponse) error {
	if !(resp.CostUSD >= 0) || resp.Usage.InputTokens < 0 || resp.Usage.OutputTokens < 0 {
		return fmt.Errorf("%w: the provider reported a cost of %v and %d/%d tokens",
			ErrProofBudget, resp.CostUSD, resp.Usage.InputTokens, resp.Usage.OutputTokens)
	}
	b.spent.USD += resp.CostUSD
	b.spent.InputTokens += int(resp.Usage.InputTokens)
	b.spent.OutputTokens += int(resp.Usage.OutputTokens)
	return nil
}

// budgetModel is the model port in front of the cap. It sizes every request before the call and refuses one that could
// pass the cap, so the loop stops with a model error that wraps ErrProofBudget.
type budgetModel struct {
	inner  ports.ModelChat
	budget *runBudget
}

// Complete sizes the request, checks the cap and records what the answer cost.
func (m budgetModel) Complete(ctx context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return ports.ModelResponse{}, fmt.Errorf("size the model request: %w", err)
	}
	if err := m.budget.allow(len(body), req.MaxOutputTokens); err != nil {
		return ports.ModelResponse{}, err
	}
	resp, err := m.inner.Complete(ctx, req)
	if err != nil {
		return resp, err
	}
	if err := m.budget.add(resp); err != nil {
		return ports.ModelResponse{}, err
	}
	return resp, nil
}
