package scenarios

import "github.com/prathameshk421/flip_the_switch_hackathon/e2e"

// All returns scenarios P1 to P9 in order.
func All() []e2e.Scenario {
	return []e2e.Scenario{p1(), p2(), p3(), p4(), p5(), p6(), p7(), p8(), p9()}
}
