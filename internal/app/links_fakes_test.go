package app

import (
	"context"

	"my-bookmark/internal/store"
)

// Fakes without account links: every login is its own base account.
func (d *authDB) Link(context.Context, string) (store.AccountLink, error) {
	return store.AccountLink{}, store.ErrNotFound
}
func (d *authDB) HasLinkedEmail(context.Context, string, string) (bool, error) { return false, nil }
func (d *authDB) HasPendingInvite(context.Context, string) (bool, error)       { return false, nil }
func (d *apiTestDB) Link(context.Context, string) (store.AccountLink, error) {
	return store.AccountLink{}, store.ErrNotFound
}
func (d *apiTestDB) HasLinkedEmail(context.Context, string, string) (bool, error) { return false, nil }
func (d *apiTestDB) HasPendingInvite(context.Context, string) (bool, error)       { return false, nil }
func (d *iapTestDB) Link(context.Context, string) (store.AccountLink, error) {
	return store.AccountLink{}, store.ErrNotFound
}
func (d *iapTestDB) HasLinkedEmail(context.Context, string, string) (bool, error) { return false, nil }
func (d *iapTestDB) HasPendingInvite(context.Context, string) (bool, error)       { return false, nil }
func (d testDB) Link(context.Context, string) (store.AccountLink, error) {
	return store.AccountLink{}, store.ErrNotFound
}
func (d testDB) HasLinkedEmail(context.Context, string, string) (bool, error) { return false, nil }
func (d testDB) HasPendingInvite(context.Context, string) (bool, error)       { return false, nil }
func (d previewDB) Link(context.Context, string) (store.AccountLink, error) {
	return store.AccountLink{}, store.ErrNotFound
}
func (d previewDB) HasLinkedEmail(context.Context, string, string) (bool, error) { return false, nil }
func (d previewDB) HasPendingInvite(context.Context, string) (bool, error)       { return false, nil }
func (d *authDB) Tags(context.Context, string, int) ([]store.TagCount, error)    { return nil, nil }
func (d *authDB) ListByTag(context.Context, string, string, string, string) ([]store.Entry, string, error) {
	return nil, "", nil
}
func (d *apiTestDB) Tags(context.Context, string, int) ([]store.TagCount, error) { return nil, nil }
func (d *apiTestDB) ListByTag(context.Context, string, string, string, string) ([]store.Entry, string, error) {
	return nil, "", nil
}
func (d *iapTestDB) Tags(context.Context, string, int) ([]store.TagCount, error) { return nil, nil }
func (d *iapTestDB) ListByTag(context.Context, string, string, string, string) ([]store.Entry, string, error) {
	return nil, "", nil
}
func (d testDB) Tags(context.Context, string, int) ([]store.TagCount, error) { return nil, nil }
func (d testDB) ListByTag(context.Context, string, string, string, string) ([]store.Entry, string, error) {
	return nil, "", nil
}
func (d previewDB) Tags(context.Context, string, int) ([]store.TagCount, error) { return nil, nil }
func (d previewDB) ListByTag(context.Context, string, string, string, string) ([]store.Entry, string, error) {
	return nil, "", nil
}
