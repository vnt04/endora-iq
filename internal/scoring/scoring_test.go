package scoring

import "testing"

func TestCompute(t *testing.T) {
	tests := []struct {
		name      string
		in        Subscores
		wantValue int
		wantGrade string
		// wantClamped is the Subscores expected to be echoed back; when zero
		// it defaults to in (no clamping expected).
		wantClamped *Subscores
	}{
		{
			name:      "all 100 yields perfect A",
			in:        Subscores{100, 100, 100, 100, 100},
			wantValue: 100,
			wantGrade: "A",
		},
		{
			name:      "all 0 yields F",
			in:        Subscores{0, 0, 0, 0, 0},
			wantValue: 0,
			wantGrade: "F",
		},
		{
			// 85*.30 + 70*.20 + 60*.20 + 50*.20 + 40*.10
			// = 25.5 + 14 + 12 + 10 + 4 = 65.5 -> round half away from zero -> 66
			name:      "mixed values round half up to 66",
			in:        Subscores{Coverage: 85, Validation: 70, Security: 60, ErrorHandling: 50, Documentation: 40},
			wantValue: 66,
			wantGrade: "D",
		},
		{
			// 91*.30 + 80*.20 + 75*.20 + 88*.20 + 50*.10
			// = 27.3 + 16 + 15 + 17.6 + 5 = 80.9 -> round -> 81
			name:      "mixed values round down to 81",
			in:        Subscores{Coverage: 91, Validation: 80, Security: 75, ErrorHandling: 88, Documentation: 50},
			wantValue: 81,
			wantGrade: "B",
		},
		{
			// Only Coverage contributes: 50 * 0.30 = 15.0 -> 15
			name:      "single dimension weighted by coverage",
			in:        Subscores{Coverage: 50},
			wantValue: 15,
			wantGrade: "F",
		},
		{
			// Documentation alone: 100 * 0.10 = 10.0 -> 10
			name:      "documentation has lowest weight",
			in:        Subscores{Documentation: 100},
			wantValue: 10,
			wantGrade: "F",
		},
		{
			// 10*.30 + 30*.20 + 50*.20 + 70*.20 + 90*.10
			// = 3 + 6 + 10 + 14 + 9 = 42.0 -> 42
			name:      "ascending mix sums to 42",
			in:        Subscores{Coverage: 10, Validation: 30, Security: 50, ErrorHandling: 70, Documentation: 90},
			wantValue: 42,
			wantGrade: "F",
		},

		// --- out-of-range inputs are clamped defensively ---
		{
			name:        "above-range inputs clamp to 100",
			in:          Subscores{200, 150, 1000, 101, 100000},
			wantValue:   100,
			wantGrade:   "A",
			wantClamped: &Subscores{100, 100, 100, 100, 100},
		},
		{
			name:        "negative inputs clamp to 0",
			in:          Subscores{-10, -1, -100, -5, -50},
			wantValue:   0,
			wantGrade:   "F",
			wantClamped: &Subscores{0, 0, 0, 0, 0},
		},
		{
			// Coverage clamps 130->100, Documentation clamps -20->0.
			// 100*.30 + 80*.20 + 80*.20 + 80*.20 + 0*.10
			// = 30 + 16 + 16 + 16 + 0 = 78.0 -> 78 -> C
			name:        "partial clamp affects weighted value",
			in:          Subscores{Coverage: 130, Validation: 80, Security: 80, ErrorHandling: 80, Documentation: -20},
			wantValue:   78,
			wantGrade:   "C",
			wantClamped: &Subscores{Coverage: 100, Validation: 80, Security: 80, ErrorHandling: 80, Documentation: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compute(tt.in)

			if got.Value != tt.wantValue {
				t.Errorf("Value = %d, want %d", got.Value, tt.wantValue)
			}
			if got.Grade != tt.wantGrade {
				t.Errorf("Grade = %q, want %q", got.Grade, tt.wantGrade)
			}

			wantClamped := tt.in
			if tt.wantClamped != nil {
				wantClamped = *tt.wantClamped
			}
			if got.Subscores != wantClamped {
				t.Errorf("Subscores = %+v, want %+v", got.Subscores, wantClamped)
			}

			// Value must always stay within bounds.
			if got.Value < scoreMin || got.Value > scoreMax {
				t.Errorf("Value %d out of [%d,%d]", got.Value, scoreMin, scoreMax)
			}
		})
	}
}

// TestComputeGradeBoundaries exercises every grade band edge. Because the
// weights sum to 1.0, equal subscores produce a weighted value equal to that
// subscore, making exact boundary values easy to assert.
func TestComputeGradeBoundaries(t *testing.T) {
	tests := []struct {
		value     int
		wantGrade string
	}{
		{100, "A"},
		{90, "A"}, // A lower bound
		{89, "B"}, // just below A
		{80, "B"}, // B lower bound
		{79, "C"}, // just below B
		{70, "C"}, // C lower bound
		{69, "D"}, // just below C
		{60, "D"}, // D lower bound
		{59, "F"}, // just below D
		{0, "F"},
	}

	for _, tt := range tests {
		t.Run(gradeName(tt.value), func(t *testing.T) {
			in := Subscores{tt.value, tt.value, tt.value, tt.value, tt.value}
			got := Compute(in)

			if got.Value != tt.value {
				t.Fatalf("equal subscores %d produced Value %d, want %d", tt.value, got.Value, tt.value)
			}
			if got.Grade != tt.wantGrade {
				t.Errorf("Value %d => Grade %q, want %q", tt.value, got.Grade, tt.wantGrade)
			}
		})
	}
}

// TestWeightsSumToOne guards the invariant that the documented weights total
// exactly 1.0, which keeps the weighted aggregate inside [0,100].
func TestWeightsSumToOne(t *testing.T) {
	sum := WeightCoverage + WeightValidation + WeightSecurity + WeightErrorHandling + WeightDocumentation
	if sum != 1.0 {
		t.Fatalf("weights sum to %v, want 1.0", sum)
	}
}

// TestClamp covers the clamp helper directly, including the boundary values.
func TestClamp(t *testing.T) {
	tests := []struct {
		in, want int
	}{
		{-1, 0},
		{0, 0},
		{1, 1},
		{50, 50},
		{99, 99},
		{100, 100},
		{101, 100},
		{-999, 0},
		{999, 100},
	}
	for _, tt := range tests {
		if got := clamp(tt.in); got != tt.want {
			t.Errorf("clamp(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func gradeName(value int) string {
	switch {
	case value >= GradeAMin:
		return "A_band"
	case value >= GradeBMin:
		return "B_band"
	case value >= GradeCMin:
		return "C_band"
	case value >= GradeDMin:
		return "D_band"
	default:
		return "F_band"
	}
}
