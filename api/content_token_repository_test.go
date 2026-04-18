package api

import (
	"errors"
	"testing"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/stretchr/testify/assert"
)

func TestContentTokenErrors_WrapDBErrors(t *testing.T) {
	assert.True(t, errors.Is(ErrContentTokenNotFound, dberrors.ErrNotFound))
	assert.True(t, errors.Is(ErrContentTokenDuplicate, dberrors.ErrDuplicate))
}
