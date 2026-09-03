package support_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/support"
)

func TestSameMajor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		current   string
		candidate string
		same      bool
	}{
		{"patch apart", "1.2.3", "1.2.9", true},
		{"minor apart", "1.2.3", "1.9.0", true},
		{"major apart", "1.9.0", "2.0.0", false},
		{"zero to one", "0.2.3", "1.0.0", false},
		{"major-only pins", "24", "24.19.0", true},
		{"major-only pins apart", "24", "26", false},
		{"leading v", "v1.0.0", "1.4.0", true},
		// Unparseable on either side has no major to compare, and answering yes
		// would move a pin on a comparison that never happened.
		{"unparseable current", "lts/*", "1.0.0", false},
		{"unparseable candidate", "1.0.0", "system", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// given / when
			same := support.SameMajor(testCase.current, testCase.candidate)

			// then
			assert.Equal(t, testCase.same, same,
				"SameMajor(%q, %q)", testCase.current, testCase.candidate)
		})
	}
}

func TestAcceptsUpgrade(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		current    string
		candidate  string
		allowMajor bool
		accepted   bool
	}{
		// Not newer is refused in both modes: the major question never arises.
		{"older, majors allowed", "2.0.0", "1.9.0", true, false},
		{"older, majors refused", "2.0.0", "1.9.0", false, false},
		{"identical", "1.0.0", "1.0.0", true, false},

		{"minor bump, majors allowed", "1.2.3", "1.9.0", true, true},
		{"minor bump, majors refused", "1.2.3", "1.9.0", false, true},

		{"major bump, majors allowed", "1.9.0", "2.0.0", true, true},
		{"major bump, majors refused", "1.9.0", "2.0.0", false, false},
		{"v0 to v1, majors refused", "0.2.3", "1.0.0", false, false},
		{"v0 to v1, majors allowed", "0.2.3", "1.0.0", true, true},

		// A pre-release still loses to the release it precedes, whatever the mode.
		{"pre-release of the same major", "1.0.0", "1.0.0-rc1", true, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// given / when
			accepted := support.AcceptsUpgrade(
				testCase.current, testCase.candidate, testCase.allowMajor,
			)

			// then
			assert.Equal(t, testCase.accepted, accepted,
				"AcceptsUpgrade(%q, %q, %t)",
				testCase.current, testCase.candidate, testCase.allowMajor)
		})
	}
}
