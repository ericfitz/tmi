package dberrors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyOracleCode_UniqueConstraint(t *testing.T) {
	src := fmt.Errorf("ORA-00001: unique constraint violated")
	err := classifyOracleCode(src, 1)
	assert.True(t, errors.Is(err, ErrDuplicate))
	assert.True(t, errors.Is(err, ErrConstraint))
}

func TestClassifyOracleCode_ForeignKey(t *testing.T) {
	src := fmt.Errorf("ORA-02291: integrity constraint violated - parent key not found")
	err := classifyOracleCode(src, 2291)
	assert.True(t, errors.Is(err, ErrForeignKey))
	assert.True(t, errors.Is(err, ErrConstraint))

	src2 := fmt.Errorf("ORA-02292: integrity constraint violated - child record found")
	err2 := classifyOracleCode(src2, 2292)
	assert.True(t, errors.Is(err2, ErrForeignKey))
}

func TestClassifyOracleCode_ValueTooLargeForColumn(t *testing.T) {
	src := fmt.Errorf("ORA-12899: value too large for column")
	err := classifyOracleCode(src, 12899)
	assert.True(t, errors.Is(err, ErrConstraint))
	assert.False(t, errors.Is(err, ErrDuplicate))
	assert.False(t, errors.Is(err, ErrForeignKey))
}

func TestClassifyOracleCode_NotNullViolation(t *testing.T) {
	src := fmt.Errorf("ORA-01400: cannot insert NULL into (\"X\".\"Y\".\"Z\")")
	err := classifyOracleCode(src, 1400)
	assert.True(t, errors.Is(err, ErrConstraint))
	assert.False(t, errors.Is(err, ErrForeignKey))
}

func TestClassifyOracleCode_CheckConstraintViolated(t *testing.T) {
	src := fmt.Errorf("ORA-02290: check constraint (X.Y) violated")
	err := classifyOracleCode(src, 2290)
	assert.True(t, errors.Is(err, ErrConstraint))
	assert.False(t, errors.Is(err, ErrForeignKey))
}

func TestClassifyOracleCode_ResourceBusy(t *testing.T) {
	src := fmt.Errorf("ORA-00054: resource busy and acquire with NOWAIT specified")
	err := classifyOracleCode(src, 54)
	assert.True(t, errors.Is(err, ErrTransient))
}

func TestClassifyOracleCode_TNSConnectionClosed(t *testing.T) {
	src := fmt.Errorf("ORA-12537: TNS:connection closed")
	err := classifyOracleCode(src, 12537)
	assert.True(t, errors.Is(err, ErrTransient))
}

func TestClassifyOracleCode_PackageStateDiscarded(t *testing.T) {
	src := fmt.Errorf("ORA-04068: existing state of packages has been discarded")
	err := classifyOracleCode(src, 4068)
	assert.True(t, errors.Is(err, ErrTransient))
}

func TestClassifyOracleCode_SerializationFailure(t *testing.T) {
	src := fmt.Errorf("ORA-08177: can't serialize access for this transaction")
	err := classifyOracleCode(src, 8177)
	assert.True(t, errors.Is(err, ErrTransient))
}

func TestClassifyOracleCode_Deadlock(t *testing.T) {
	src := fmt.Errorf("ORA-00060: deadlock detected")
	err := classifyOracleCode(src, 60)
	assert.True(t, errors.Is(err, ErrTransient))
}

func TestClassifyOracleCode_ConnectionErrors(t *testing.T) {
	for _, code := range []int{3113, 3114, 3135, 12170, 12541, 12543} {
		src := fmt.Errorf("ORA-%05d: connection error", code)
		err := classifyOracleCode(src, code)
		assert.True(t, errors.Is(err, ErrTransient), "code %d should be transient", code)
	}
}

func TestClassifyOracleCode_PermissionErrors(t *testing.T) {
	src := fmt.Errorf("ORA-01017: invalid username/password")
	err := classifyOracleCode(src, 1017)
	assert.True(t, errors.Is(err, ErrPermission))

	src2 := fmt.Errorf("ORA-01031: insufficient privileges")
	err2 := classifyOracleCode(src2, 1031)
	assert.True(t, errors.Is(err2, ErrPermission))
}

func TestClassifyOracleCode_LogonDenied(t *testing.T) {
	src := fmt.Errorf("ORA-01045: user X lacks CREATE SESSION privilege; logon denied")
	err := classifyOracleCode(src, 1045)
	assert.True(t, errors.Is(err, ErrPermission))
}

func TestClassifyOracleCode_PasswordExpired(t *testing.T) {
	src := fmt.Errorf("ORA-28001: the password has expired")
	err := classifyOracleCode(src, 28001)
	assert.True(t, errors.Is(err, ErrPermission))
}

func TestClassifyOracleCode_UserRequestedCancel(t *testing.T) {
	src := fmt.Errorf("ORA-01013: user requested cancel of current operation")
	err := classifyOracleCode(src, 1013)
	assert.True(t, errors.Is(err, ErrContextDone))
}

func TestClassifyOracleCode_AdditionalTransientCodes(t *testing.T) {
	for _, code := range []int{18, 20, 3156, 12519, 12520, 25408} {
		src := fmt.Errorf("ORA-%05d: synthetic transient", code)
		err := classifyOracleCode(src, code)
		assert.True(t, errors.Is(err, ErrTransient), "code %d should be transient", code)
	}
}

func TestClassifyOracleCode_SnapshotTooOldNotClassified(t *testing.T) {
	// ORA-01555 is intentionally NOT classified — single-statement retry won't help.
	// Caller falls through to string fallback or surfaces as unclassified.
	src := fmt.Errorf("ORA-01555: snapshot too old")
	err := classifyOracleCode(src, 1555)
	assert.Nil(t, err, "ORA-01555 should not produce a typed sentinel (caller decides handling)")
}

func TestClassifyOracleCode_UnknownCode(t *testing.T) {
	src := fmt.Errorf("ORA-99999: synthetic test code")
	err := classifyOracleCode(src, 99999)
	assert.Nil(t, err, "unknown ORA codes should return nil so caller can fall through")
}
