package testutil

import "github.com/leightonvanrooijen/utopia/internal/domain"

// CRWithFeatures creates a ChangeRequest with add operations for the given features.
// This is a test helper to reduce boilerplate when setting up test fixtures.
func CRWithFeatures(id string, features ...domain.Feature) *domain.ChangeRequest {
	cr := &domain.ChangeRequest{
		ID:    id,
		Type:  domain.CRTypeFeature,
		Title: "Test CR",
	}
	for _, f := range features {
		cr.Changes = append(cr.Changes, domain.Change{
			Operation: "add",
			Feature:   &f,
		})
	}
	return cr
}
