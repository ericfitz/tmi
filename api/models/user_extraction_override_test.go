package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUser_ExtractionConcurrencyOverride_Nullable(t *testing.T) {
	u := User{}
	assert.Nil(t, u.ExtractionConcurrencyOverride, "default must be nil (no override)")

	v := 8
	u.ExtractionConcurrencyOverride = &v
	if u.ExtractionConcurrencyOverride == nil {
		t.Fatalf("override unexpectedly nil after assignment")
	}
	assert.Equal(t, 8, *u.ExtractionConcurrencyOverride)
}
