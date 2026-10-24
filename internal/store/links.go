package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
)

// MaxLinkedAccounts is the number of accounts a base account may link.
const MaxLinkedAccounts = 5

var ErrLinkLimit = errors.New("linked account limit reached")
var ErrLinkRejected = errors.New("account cannot be linked")
var ErrEmailMismatch = errors.New("invite is for a different email")

// AccountLink maps a login identity onto the base account whose data it uses.
type AccountLink struct {
	LoginID   string    `firestore:"-"`
	BaseID    string    `firestore:"base_id"`
	Email     string    `firestore:"email"`
	CreatedAt time.Time `firestore:"created_at"`
}

// AccountInvite is a pending, single-use invitation keyed by the hash of its token.
type AccountInvite struct {
	ID        string    `firestore:"-"`
	BaseID    string    `firestore:"base_id"`
	Email     string    `firestore:"email"`
	ExpiresAt time.Time `firestore:"expires_at"`
}

func (s *Store) links() *firestore.CollectionRef   { return s.client.Collection("account_links") }
func (s *Store) invites() *firestore.CollectionRef { return s.client.Collection("account_invites") }

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// Link returns the link for a login identity; ErrNotFound means it is its own base.
func (s *Store) Link(ctx context.Context, loginID string) (AccountLink, error) {
	d, err := s.links().Doc(loginID).Get(ctx)
	if err != nil {
		return AccountLink{}, normalizeError(err)
	}
	var link AccountLink
	err = d.DataTo(&link)
	link.LoginID = d.Ref.ID
	return link, err
}

// UserName returns the display name recorded for a user ID.
func (s *Store) UserName(ctx context.Context, uid string) (string, error) {
	d, err := s.client.Collection("users").Doc(uid).Get(ctx)
	if err != nil {
		return "", normalizeError(err)
	}
	var u User
	err = d.DataTo(&u)
	return u.Name, err
}

func linksOf(docs []*firestore.DocumentSnapshot) ([]AccountLink, error) {
	out := make([]AccountLink, 0, len(docs))
	for _, d := range docs {
		var link AccountLink
		if err := d.DataTo(&link); err != nil {
			return nil, err
		}
		link.LoginID = d.Ref.ID
		out = append(out, link)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func invitesOf(docs []*firestore.DocumentSnapshot) ([]AccountInvite, error) {
	out := make([]AccountInvite, 0, len(docs))
	for _, d := range docs {
		var invite AccountInvite
		if err := d.DataTo(&invite); err != nil {
			return nil, err
		}
		invite.ID = d.Ref.ID
		out = append(out, invite)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ExpiresAt.Before(out[j].ExpiresAt) })
	return out, nil
}

func (s *Store) LinkedAccounts(ctx context.Context, baseID string) ([]AccountLink, error) {
	docs, err := s.links().Where("base_id", "==", baseID).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	return linksOf(docs)
}

// PendingInvites lists the unexpired invitations created by a base account.
func (s *Store) PendingInvites(ctx context.Context, baseID string) ([]AccountInvite, error) {
	docs, err := s.invites().Where("base_id", "==", baseID).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	all, err := invitesOf(docs)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	live := all[:0]
	for _, invite := range all {
		if invite.ExpiresAt.After(now) {
			live = append(live, invite)
		}
	}
	return live, nil
}

// ReapExpiredInvites deletes a base account's expired invitations and returns their emails.
func (s *Store) ReapExpiredInvites(ctx context.Context, baseID string) ([]string, error) {
	docs, err := s.invites().Where("base_id", "==", baseID).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	all, err := invitesOf(docs)
	if err != nil {
		return nil, err
	}
	var emails []string
	for _, invite := range all {
		if invite.ExpiresAt.After(time.Now()) {
			continue
		}
		if _, err = s.invites().Doc(invite.ID).Delete(ctx); err != nil {
			return emails, err
		}
		emails = append(emails, invite.Email)
	}
	return emails, nil
}

// CreateInvite records an invitation. Pending invitations count toward the limit,
// and a repeated invitation to the same email replaces the earlier one.
func (s *Store) CreateInvite(ctx context.Context, baseID, email, token string, ttl time.Duration) error {
	email = normalizeEmail(email)
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		baseLink, err := tx.Get(s.links().Doc(baseID))
		if err == nil && baseLink.Exists() {
			return ErrLinkRejected
		}
		if err != nil && !missing(err) {
			return err
		}
		linkDocs, err := tx.Documents(s.links().Where("base_id", "==", baseID)).GetAll()
		if err != nil {
			return err
		}
		linked, err := linksOf(linkDocs)
		if err != nil {
			return err
		}
		for _, link := range linked {
			if link.Email == email {
				return ErrLinkRejected
			}
		}
		inviteDocs, err := tx.Documents(s.invites().Where("base_id", "==", baseID)).GetAll()
		if err != nil {
			return err
		}
		pending, err := invitesOf(inviteDocs)
		if err != nil {
			return err
		}
		count := len(linked)
		var replaced []string
		for _, invite := range pending {
			switch {
			case invite.Email == email:
				replaced = append(replaced, invite.ID)
			case invite.ExpiresAt.After(time.Now()):
				count++
			}
		}
		if count >= MaxLinkedAccounts {
			return ErrLinkLimit
		}
		for _, id := range replaced {
			if err = tx.Delete(s.invites().Doc(id)); err != nil {
				return err
			}
		}
		return tx.Create(s.invites().Doc(Hash(token)), AccountInvite{BaseID: baseID, Email: email, ExpiresAt: time.Now().Add(ttl)})
	})
}

func (s *Store) DeleteInvite(ctx context.Context, token string) error {
	_, err := s.invites().Doc(Hash(token)).Delete(ctx)
	return err
}

// Invite returns an unexpired invitation without consuming it.
func (s *Store) Invite(ctx context.Context, token string) (AccountInvite, error) {
	d, err := s.invites().Doc(Hash(token)).Get(ctx)
	if err != nil {
		return AccountInvite{}, normalizeError(err)
	}
	var invite AccountInvite
	if err = d.DataTo(&invite); err != nil {
		return invite, err
	}
	invite.ID = d.Ref.ID
	if !invite.ExpiresAt.After(time.Now()) {
		return AccountInvite{}, ErrNotFound
	}
	return invite, nil
}

// ConfirmInvite links the login identity that owns the invited email to the
// inviting base account. The invitation is consumed in the same transaction.
func (s *Store) ConfirmInvite(ctx context.Context, token, loginID, email string) (AccountLink, error) {
	var link AccountLink
	err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ref := s.invites().Doc(Hash(token))
		d, err := tx.Get(ref)
		if err != nil {
			return normalizeError(err)
		}
		var invite AccountInvite
		if err = d.DataTo(&invite); err != nil {
			return err
		}
		if !invite.ExpiresAt.After(time.Now()) {
			return ErrNotFound
		}
		if normalizeEmail(email) == "" || normalizeEmail(email) != invite.Email {
			return ErrEmailMismatch
		}
		if loginID == invite.BaseID {
			return ErrLinkRejected
		}
		for _, id := range []string{loginID, invite.BaseID} {
			doc, err := tx.Get(s.links().Doc(id))
			if err == nil && doc.Exists() {
				return ErrLinkRejected
			}
			if err != nil && !missing(err) {
				return err
			}
		}
		owned, err := tx.Documents(s.links().Where("base_id", "==", loginID).Limit(1)).GetAll()
		if err != nil {
			return err
		}
		if len(owned) > 0 {
			return ErrLinkRejected
		}
		existing, err := tx.Documents(s.links().Where("base_id", "==", invite.BaseID)).GetAll()
		if err != nil {
			return err
		}
		if len(existing) >= MaxLinkedAccounts {
			return ErrLinkLimit
		}
		link = AccountLink{LoginID: loginID, BaseID: invite.BaseID, Email: invite.Email, CreatedAt: time.Now()}
		if err = tx.Create(s.links().Doc(loginID), link); err != nil {
			return err
		}
		return tx.Delete(ref)
	})
	return link, err
}

// Unlink removes a link owned by baseID and returns it so access can be revoked.
func (s *Store) Unlink(ctx context.Context, baseID, loginID string) (AccountLink, error) {
	var link AccountLink
	err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ref := s.links().Doc(loginID)
		d, err := tx.Get(ref)
		if err != nil {
			return normalizeError(err)
		}
		if err = d.DataTo(&link); err != nil {
			return err
		}
		if link.BaseID != baseID {
			return ErrNotFound
		}
		link.LoginID = loginID
		return tx.Delete(ref)
	})
	return link, err
}

// HasLinkedEmail reports whether baseID has a linked account with this email.
func (s *Store) HasLinkedEmail(ctx context.Context, baseID, email string) (bool, error) {
	docs, err := s.links().Where("base_id", "==", baseID).Where("email", "==", normalizeEmail(email)).Limit(1).Documents(ctx).GetAll()
	return len(docs) > 0, err
}

// HasPendingInvite reports whether an unexpired invitation exists for the email.
func (s *Store) HasPendingInvite(ctx context.Context, email string) (bool, error) {
	docs, err := s.invites().Where("email", "==", normalizeEmail(email)).Documents(ctx).GetAll()
	if err != nil {
		return false, err
	}
	invites, err := invitesOf(docs)
	if err != nil {
		return false, err
	}
	for _, invite := range invites {
		if invite.ExpiresAt.After(time.Now()) {
			return true, nil
		}
	}
	return false, nil
}

// EmailInUse reports whether any link or live invitation still needs access for the email.
func (s *Store) EmailInUse(ctx context.Context, email string) (bool, error) {
	docs, err := s.links().Where("email", "==", normalizeEmail(email)).Limit(1).Documents(ctx).GetAll()
	if err != nil || len(docs) > 0 {
		return len(docs) > 0, err
	}
	return s.HasPendingInvite(ctx, email)
}
