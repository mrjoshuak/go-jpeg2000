//go:build race

package jpeg2000

// allocBudgetScale multiplies the allocation budget the malformed-input tests
// enforce. The race detector adds shadow state to every allocation, so the
// same decode charges several times as many bytes; the test is measuring the
// decoder's amplification factor, not the instrumentation's.
const allocBudgetScale = 16
