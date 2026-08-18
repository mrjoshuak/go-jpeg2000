//go:build !race

package jpeg2000

// allocBudgetScale multiplies the allocation budget the malformed-input tests
// enforce. Without the race detector the budget is used as written.
const allocBudgetScale = 1
