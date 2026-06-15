// Package scoring is the deterministic API quality-score engine (SRS §16). It
// combines weighted subscores into a single 0-100 value and a letter grade. It
// uses NO AI: the result is pure, deterministic Go and unit-testable without
// API keys.
package scoring

import "math"

// Subscores are the per-dimension inputs to the quality score, each 0-100.
type Subscores struct {
	Coverage      int
	Validation    int
	Security      int
	ErrorHandling int
	Documentation int
}

// Score is the computed quality score and its letter grade.
type Score struct {
	Value     int       // weighted 0-100
	Grade     string    // "A" | "B" | "C" | "D" | "F"
	Subscores Subscores // the (clamped) inputs, echoed back for reporting
}

// Dimension weights. Each subscore contributes proportionally to the final
// value; the weights are documented and intentionally sum to exactly 1.0 so the
// weighted aggregate stays within the same [0,100] range as its inputs.
//
//	Coverage      0.30  // breadth of scenarios exercised dominates quality
//	Validation    0.20  // input/output assertion strength
//	Security      0.20  // auth/secret handling rigor
//	ErrorHandling 0.20  // negative-path and error-status coverage
//	Documentation 0.10  // descriptions/examples; least weighted
const (
	WeightCoverage      = 0.30
	WeightValidation    = 0.20
	WeightSecurity      = 0.20
	WeightErrorHandling = 0.20
	WeightDocumentation = 0.10
)

// Grade band lower bounds (inclusive), applied to the final weighted Value.
//
//	A >= 90, B >= 80, C >= 70, D >= 60, else F.
const (
	GradeAMin = 90
	GradeBMin = 80
	GradeCMin = 70
	GradeDMin = 60
)

// scoreMin and scoreMax are the inclusive bounds every subscore and the final
// value are clamped to.
const (
	scoreMin = 0
	scoreMax = 100
)

// Compute folds the subscores into a weighted quality Score. Each incoming
// subscore is defensively clamped to [0,100], combined using the documented
// dimension weights, rounded to the nearest integer, and clamped again. The
// returned Score echoes the clamped subscores so reports show the values that
// were actually scored.
func Compute(s Subscores) Score {
	clamped := Subscores{
		Coverage:      clamp(s.Coverage),
		Validation:    clamp(s.Validation),
		Security:      clamp(s.Security),
		ErrorHandling: clamp(s.ErrorHandling),
		Documentation: clamp(s.Documentation),
	}

	weighted := float64(clamped.Coverage)*WeightCoverage +
		float64(clamped.Validation)*WeightValidation +
		float64(clamped.Security)*WeightSecurity +
		float64(clamped.ErrorHandling)*WeightErrorHandling +
		float64(clamped.Documentation)*WeightDocumentation

	value := clamp(int(math.Round(weighted)))

	return Score{
		Value:     value,
		Grade:     grade(value),
		Subscores: clamped,
	}
}

// clamp constrains v to the inclusive [scoreMin, scoreMax] range.
func clamp(v int) int {
	switch {
	case v < scoreMin:
		return scoreMin
	case v > scoreMax:
		return scoreMax
	default:
		return v
	}
}

// grade maps a 0-100 value to its letter band.
func grade(value int) string {
	switch {
	case value >= GradeAMin:
		return "A"
	case value >= GradeBMin:
		return "B"
	case value >= GradeCMin:
		return "C"
	case value >= GradeDMin:
		return "D"
	default:
		return "F"
	}
}
