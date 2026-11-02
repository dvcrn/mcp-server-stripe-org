package stripe

import (
	"os"
	"regexp"
	"testing"
)

// probe-permissions.sh is standalone bash and repeats the client's API versions.
func TestProbeScriptVersionsMatchClient(t *testing.T) {
	script, err := os.ReadFile("../scripts/probe-permissions.sh")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"STRIPE_VERSION":  APIVersion,
		"PREVIEW_VERSION": PreviewAPIVersion,
	} {
		m := regexp.MustCompile(`(?m)^readonly ` + name + `="([^"]*)"$`).FindSubmatch(script)
		if m == nil {
			t.Errorf("probe-permissions.sh: readonly %s not found", name)
			continue
		}
		if got := string(m[1]); got != want {
			t.Errorf("probe-permissions.sh %s = %q, want %q", name, got, want)
		}
	}
}
