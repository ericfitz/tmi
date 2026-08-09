package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/ericfitz/tmi/auth/repository"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// notFoundUserRepo implements repository.UserRepository, returning
// repository.ErrUserNotFound from every lookup and mutation the Service
// delegates to (#719).
// SEM@cf201bc8c1eab7bf74de941e50508142b759ca75: fake UserRepository whose lookups and mutations all report not-found (pure)
type notFoundUserRepo struct{}

// SEM@cf201bc8c1eab7bf74de941e50508142b759ca75: stub a user email lookup that always returns not-found (pure)
func (notFoundUserRepo) GetByEmail(context.Context, string) (*repository.User, error) {
	return nil, repository.ErrUserNotFound
}

// SEM@cf201bc8c1eab7bf74de941e50508142b759ca75: stub a provider-ID user lookup that always returns not-found (pure)
func (notFoundUserRepo) GetByProviderID(context.Context, string, string) (*repository.User, error) {
	return nil, repository.ErrUserNotFound
}

// SEM@cf201bc8c1eab7bf74de941e50508142b759ca75: stub a provider-and-email user lookup that always returns not-found (pure)
func (notFoundUserRepo) GetByProviderAndEmail(context.Context, string, string) (*repository.User, error) {
	return nil, repository.ErrUserNotFound
}

// SEM@cf201bc8c1eab7bf74de941e50508142b759ca75: stub a cross-provider user lookup that always returns not-found (pure)
func (notFoundUserRepo) GetByAnyProviderID(context.Context, string) (*repository.User, error) {
	return nil, repository.ErrUserNotFound
}

// SEM@cf201bc8c1eab7bf74de941e50508142b759ca75: stub a user ID lookup that always returns not-found (pure)
func (notFoundUserRepo) GetByID(context.Context, string) (*repository.User, error) {
	return nil, repository.ErrUserNotFound
}

// SEM@cf201bc8c1eab7bf74de941e50508142b759ca75: stub a provider list lookup that returns an empty result (pure)
func (notFoundUserRepo) GetProviders(context.Context, string) ([]repository.UserProvider, error) {
	return nil, nil
}

// SEM@cf201bc8c1eab7bf74de941e50508142b759ca75: stub a user repository provider-ID lookup that always returns not-found (pure)
func (notFoundUserRepo) GetPrimaryProviderID(context.Context, string) (string, error) {
	return "", repository.ErrUserNotFound
}

// SEM@cf201bc8c1eab7bf74de941e50508142b759ca75: stub a no-op user repository create for a not-found test double (pure)
func (notFoundUserRepo) Create(context.Context, *repository.User) (*repository.User, error) {
	return nil, nil
}

// SEM@cf201bc8c1eab7bf74de941e50508142b759ca75: stub a user repository update that always returns not-found (pure)
func (notFoundUserRepo) Update(context.Context, *repository.User) error {
	return repository.ErrUserNotFound
}

// SEM@cf201bc8c1eab7bf74de941e50508142b759ca75: stub a user repository delete that always returns not-found (pure)
func (notFoundUserRepo) Delete(context.Context, string) error {
	return repository.ErrUserNotFound
}

// deleteNotFoundUserRepo lets GetByID succeed so Service.DeleteUser reaches
// the repo-level delete, which then reports repository.ErrUserNotFound.
// SEM@cf201bc8c1eab7bf74de941e50508142b759ca75: define a user repository test double where only delete returns not-found (pure)
type deleteNotFoundUserRepo struct{ notFoundUserRepo }

// SEM@cf201bc8c1eab7bf74de941e50508142b759ca75: stub a user repository fetch by ID returning a fixed target user (pure)
func (deleteNotFoundUserRepo) GetByID(context.Context, string) (*repository.User, error) {
	return &repository.User{InternalUUID: "target-uuid", Email: "target@example.com"}, nil
}

// assertTypedNotFound checks the #719 guarantees shared by every Service
// user-lookup not-found return: errors.Is at both the auth and repository
// level, dberrors classification, and the historical message prefix.
// SEM@cf201bc8c1eab7bf74de941e50508142b759ca75: validate that an error carries the typed not-found guarantees across auth, repository, and dberrors layers (pure)
func assertTypedNotFound(t *testing.T, name string, err error) {
	t.Helper()
	require.Error(t, err, name)
	assert.True(t, errors.Is(err, ErrUserNotFound), "%s: errors.Is(err, auth.ErrUserNotFound)", name)
	assert.True(t, errors.Is(err, repository.ErrUserNotFound), "%s: errors.Is(err, repository.ErrUserNotFound)", name)
	assert.True(t, errors.Is(dberrors.Classify(err), dberrors.ErrNotFound), "%s: classifies as ErrNotFound", name)
	assert.Contains(t, err.Error(), "user not found", "%s: message keeps historical prefix", name)
}

// #719 — Service lookups must propagate a typed not-found, not a bare string.
// SEM@cf201bc8c1eab7bf74de941e50508142b759ca75: test that Service user-lookup methods return a typed not-found error for a missing user
func TestServiceUserLookups_ReturnTypedNotFound(t *testing.T) {
	svc, cleanup := setupTestServiceWithRepos(t, notFoundUserRepo{}, nil)
	defer cleanup()
	ctx := context.Background()

	calls := map[string]func() error{
		"GetUserByEmail":            func() error { _, err := svc.GetUserByEmail(ctx, "a@b.c"); return err },
		"GetUserByID":               func() error { _, err := svc.GetUserByID(ctx, "uuid"); return err },
		"GetUserByProviderID":       func() error { _, err := svc.GetUserByProviderID(ctx, "p", "id"); return err },
		"GetUserByProviderAndEmail": func() error { _, err := svc.GetUserByProviderAndEmail(ctx, "p", "a@b.c"); return err },
		"GetUserByAnyProviderID":    func() error { _, err := svc.GetUserByAnyProviderID(ctx, "id"); return err },
		"UpdateUser":                func() error { return svc.UpdateUser(ctx, User{InternalUUID: "uuid"}) },
	}
	for name, call := range calls {
		assertTypedNotFound(t, name, call())
	}
}

// #719 — DeleteUser's own not-found branch (repo-level delete, not the
// GetUserByID precheck) must also return the typed sentinel.
// SEM@cf201bc8c1eab7bf74de941e50508142b759ca75: test that Service.DeleteUser's repo-level delete branch returns a typed not-found error
func TestServiceDeleteUser_ReturnsTypedNotFound(t *testing.T) {
	svc, cleanup := setupTestServiceWithRepos(t, deleteNotFoundUserRepo{}, nil)
	defer cleanup()

	err := svc.DeleteUser(context.Background(), "target-uuid")
	assertTypedNotFound(t, "DeleteUser", err)
}
