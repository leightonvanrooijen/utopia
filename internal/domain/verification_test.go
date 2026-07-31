package domain

import (
	"strings"
	"testing"
)

// The retry count bounds how many extra attempts a validator gets when its
// invocation never ran. Zero is legitimate - retry nothing - so only a negative
// value, which describes nothing the runner could do, is rejected.
func TestValidateVerificationConfig(t *testing.T) {
	retries := func(n int) *int { return &n }

	tests := []struct {
		name    string
		config  VerificationConfig
		wantErr bool
	}{
		{name: "omitted takes the default", config: VerificationConfig{}},
		{name: "zero disables retrying", config: VerificationConfig{ValidatorInvocationRetries: retries(0)}},
		{name: "a positive count is a bound", config: VerificationConfig{ValidatorInvocationRetries: retries(4)}},
		{name: "a negative count is rejected", config: VerificationConfig{ValidatorInvocationRetries: retries(-1)}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVerificationConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateVerificationConfig = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "verification.validator_invocation_retries") {
				t.Errorf("error = %q, want it to name the offending key", err)
			}
		})
	}
}
