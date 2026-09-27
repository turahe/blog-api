package requests

import (
	"strconv"
	"testing"
)

func TestStoreConsentValidation(t *testing.T) {
	t.Parallel()

	eleven := make(map[string]bool, 11)
	for i := range 11 {
		eleven["p"+strconv.Itoa(i)] = true
	}

	base := map[string]any{"purposes": map[string]bool{"analytics": true}, "policyVersion": "2026-01"}

	runBindCases[StoreConsent](t, []bindCase{
		{name: "valid", body: with(base)},
		{name: "purposes required", body: with(base, "purposes", absent), want: errs("purposes", msgRequired("purposes"))},
		{name: "purposes empty", body: with(base, "purposes", map[string]bool{}), want: errs("purposes", msgMinItems("purposes", 1))},
		{name: "too many purposes", body: with(base, "purposes", eleven), want: errs("purposes", msgMaxItems("purposes", 10))},
		{name: "policyVersion required", body: with(base, "policyVersion", absent), want: errs("policyVersion", msgRequired("policyVersion"))},
		{name: "policyVersion too long", body: with(base, "policyVersion", long(33)), want: errs("policyVersion", msgMaxChars("policyVersion", 32))},
	})
}
