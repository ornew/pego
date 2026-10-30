package gogen

// The tests of package gogen_test compile grammars with the engine, which imports packages that
// the generator must not depend on, so they cannot live in package gogen. These declarations
// expose to them the unexported internals that they check.

type (
	Dvar         = dvar
	FlowVars     = flowVars
	LiveRules    = liveRules
	RuleKey      = ruleKey
	ShakeUnit    = shakeUnit
	TypesPackage = typesPackage
	VarStmt      = varStmt
)

const RuntimeImportPath = runtimeImportPath

var (
	ClassBits          = classBits
	DeclaredNames      = declaredNames
	DefaultPackageName = defaultPackageName
	FreeIdents         = freeIdents
	KeptUnits          = keptUnits
	LoadRuntime        = loadRuntime
	NewFlowVars        = newFlowVars
	QualifyRuntime     = qualifyRuntime
	RuleTableLimits    = ruleTableLimits
	ShakableVar        = shakableVar
	ShakeVars          = shakeVars
	TrimComments       = trimComments
	VarLinesExcept     = varLinesExcept
)

// ReachFrom calls reachFrom.
func (l *LiveRules) ReachFrom(roots []RuleKey) { l.reachFrom(roots) }

// MissingBodies calls missingBodies.
func (l *LiveRules) MissingBodies(table string, printed map[RuleKey]bool) []RuleKey {
	return l.missingBodies(table, printed)
}
