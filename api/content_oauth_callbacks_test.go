package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAllowList_ExactMatch(t *testing.T) {
	al := NewClientCallbackAllowList([]string{"http://host/cb"})
	assert.True(t, al.Allowed("http://host/cb"))
}

func TestAllowList_PrefixWildcard(t *testing.T) {
	al := NewClientCallbackAllowList([]string{"http://host/*"})
	assert.True(t, al.Allowed("http://host/cb"))
	assert.True(t, al.Allowed("http://host/app/path"))
	assert.False(t, al.Allowed("http://other/cb"))
}

func TestAllowList_NoMatch(t *testing.T) {
	al := NewClientCallbackAllowList([]string{"http://host/cb"})
	assert.False(t, al.Allowed("http://host/other"))
	assert.False(t, al.Allowed("http://other/cb"))
}

func TestAllowList_Empty(t *testing.T) {
	al := NewClientCallbackAllowList([]string{})
	assert.False(t, al.Allowed("http://host/cb"))
}
