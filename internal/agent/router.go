package agent

// RouteFor picks the rail for the given bead based on the 7 ADR §5.2 rules
// (first-match-wins). Returns the rail and the rule number that matched (1..7).
//
// Rule evaluation order:
//
//	1. Label "needs-thinking" or "extended-thinking" → Claude
//	2. Severity ∈ {CRITICAL, P0, P1}               → Claude
//	3. DeferredCount ≥ 1                            → Claude
//	4. EstLOC > 200                                 → Claude
//	5. Label ∈ {architecture, security, audit}      → Claude
//	6. PolecatRailRequest == "claude"               → Claude (M5 hook point)
//	7. (default)                                    → OpenCode
func RouteFor(b Bead) (Rail, int) {
	c := Classify(b)
	return c.Rail, c.Rule
}
