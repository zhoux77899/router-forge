package domain

import "testing"

func TestContainerAgentInstallationUsesHarborDefault(t *testing.T) {
	baseline := RunSpec{EnvironmentID: "env", ModelID: "model", Agent: "opencode", SuiteID: "swebench-verified", TaskNames: []string{"one"}, Concurrency: 1, Attempts: 1, TimeoutSec: 60, SetupTimeoutSec: 60, MaxRequests: 1, MaxOutputTokens: 1}
	if err := baseline.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"latest", "next", "canary", "1.19.4", "v1.2.3"} {
		spec := baseline
		spec.AgentVersion = version
		if spec.Validate() == nil {
			t.Fatalf("accepted explicit legacy installation request %q", version)
		}
	}
}

func TestContainerAgentVersionSupportedSubset(t *testing.T) {
	cases := []struct {
		version string
		valid   bool
	}{
		{"0.0.0", true}, {"1.2.3", true}, {"v1.2.3", true},
		{"1.2.3-beta.1", true}, {"1.2.3-0", true}, {"1.2.3-alpha01", true},
		{"1.2.3+build.5", true}, {"1.2.3+01", true},
		{"1.2", false}, {"1.2.3.4", false}, {"01.2.3", false},
		{"1.02.3", false}, {"1.2.03", false}, {"1.2.3-01", false},
		{"1.2.3-beta..1", false}, {"1.2.3-", false}, {"1.2.3+", false},
		{"1.2.3-beta.1+build.5", false}, {"1.2.3+build..5", false},
		{" 1.2.3", false}, {"1.2.3\n", false}, {"vlatest", false},
	}
	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			if got := ValidAgentVersion(tc.version); got != tc.valid {
				t.Fatalf("ValidAgentVersion(%q) = %v, want %v", tc.version, got, tc.valid)
			}
		})
	}
}
