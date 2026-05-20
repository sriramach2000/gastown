package formula

// EmbeddedFormula describes a single formula embedded in the binary.
type EmbeddedFormula struct {
	// Name is the bare formula name without the .formula.toml suffix,
	// e.g. "mol-polecat-work".
	Name string
	// Filename is the full filename including the suffix,
	// e.g. "mol-polecat-work.formula.toml".
	Filename string
}

// LoadEmbeddedFormulas returns metadata for all formulas compiled into the
// binary via go:embed. Callers that need the raw TOML bytes should use
// GetEmbeddedFormulaContent instead.
//
// This is the public entry point for enumerating the embedded formula
// registry without touching the filesystem (gastown-dogfood-wy8).
func LoadEmbeddedFormulas() ([]EmbeddedFormula, error) {
	entries, err := formulasFS.ReadDir("formulas")
	if err != nil {
		return nil, err
	}

	result := make([]EmbeddedFormula, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		bare := name
		if hasFormulaSuffix(bare) {
			bare = bare[:len(bare)-len(".formula.toml")]
		}
		result = append(result, EmbeddedFormula{
			Name:     bare,
			Filename: name,
		})
	}
	return result, nil
}
