package main

import (
	"strings"
	"testing"
)

func TestAConfiguredAllowlistIsSaidToBeIgnored(t *testing.T) {
	notice, retired := sandboxSecretsRetired("GH_TOKEN,LINEAR_API_KEY")
	if !retired {
		t.Fatal("a crew configured with QC_SANDBOX_SECRETS said nothing about it")
	}
	for _, want := range []string{"QC_SANDBOX_SECRETS", "no longer read", "configuration"} {
		if !strings.Contains(notice, want) {
			t.Errorf("the notice does not mention %q: %s", want, notice)
		}
	}
}

func TestNothingIsSaidWhenTheAllowlistWasNeverSet(t *testing.T) {
	if _, retired := sandboxSecretsRetired(""); retired {
		t.Error("a crew with no QC_SANDBOX_SECRETS was told about one")
	}
	// Whitespace is how a commented out line comes back from compose, and a warning about a setting
	// the operator has already removed is noise.
	if _, retired := sandboxSecretsRetired("   "); retired {
		t.Error("a blank QC_SANDBOX_SECRETS was read as configured")
	}
}
