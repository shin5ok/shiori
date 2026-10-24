package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"my-bookmark/internal/iap"
	"my-bookmark/internal/store"
)

const inviteTTL = 24 * time.Hour

// Mailer delivers account-linking invitations.
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

// AccessGranter opens and closes IAP access for invited and linked accounts.
type AccessGranter interface {
	Grant(ctx context.Context, email string) error
	Revoke(ctx context.Context, email string) error
}

// resolveLogin maps a login identity to the base account whose data it uses.
func (a *App) resolveLogin(ctx context.Context, loginID string) (base, baseName string, linked bool, err error) {
	link, err := a.db.Link(ctx, loginID)
	if errors.Is(err, store.ErrNotFound) {
		return loginID, "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	name, err := a.db.UserName(ctx, link.BaseID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", "", false, err
	}
	return link.BaseID, name, true, nil
}

// emailAllowed reports whether the session's email may use the service: it is
// allowlisted in config.yaml or is a linked account of the session's user.
func (a *App) emailAllowed(ctx context.Context, uid, email string) (bool, error) {
	if a.cfg.IAPAllowlist.Allows(email) {
		return true, nil
	}
	return a.db.HasLinkedEmail(ctx, uid, email)
}

func isBase(s *store.Session) bool { return s.LoginID == "" || s.LoginID == s.User.ID }

type AccountsPage struct {
	Linked   []store.AccountLink
	Pending  []store.AccountInvite
	Max      int
	Enabled  bool
	Notice   string
	NotBase  bool
	Confirm  *store.AccountInvite
	Token    string
	BaseName string
}

func (a *App) linkingEnabled() bool { return a.mailer != nil && a.granter != nil }

// revokeUnused removes IAP access once no link or invitation needs it.
func (a *App) revokeUnused(ctx context.Context, email string) error {
	if a.granter == nil || a.cfg.IAPAllowlist.Allows(email) {
		return nil
	}
	inUse, err := a.db.EmailInUse(ctx, email)
	if err != nil || inUse {
		return err
	}
	return a.granter.Revoke(ctx, email)
}

func (a *App) accountSettings(w http.ResponseWriter, r *http.Request) {
	a.renderAccounts(w, r, 200, "")
}

func (a *App) renderAccounts(w http.ResponseWriter, r *http.Request, status int, notice string) {
	s := session(r)
	page := AccountsPage{Max: store.MaxLinkedAccounts, Enabled: a.linkingEnabled(), Notice: notice, NotBase: !isBase(s)}
	if !page.NotBase {
		// Expired invitations are cleaned up here so their IAP access does not linger.
		expired, err := a.db.ReapExpiredInvites(r.Context(), s.User.ID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		for _, email := range expired {
			if err = a.revokeUnused(r.Context(), email); err != nil {
				a.fail(w, r, err)
				return
			}
		}
		if page.Linked, err = a.db.LinkedAccounts(r.Context(), s.User.ID); err != nil {
			a.fail(w, r, err)
			return
		}
		if page.Pending, err = a.db.PendingInvites(r.Context(), s.User.ID); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	w.Header().Set("Referrer-Policy", "same-origin")
	a.render(w, r, status, Page{Title: "アカウントの名寄せ", AccountSettings: true, Accounts: &page})
}

func (a *App) inviteAccount(w http.ResponseWriter, r *http.Request) {
	s := session(r)
	if !isBase(s) || !a.linkingEnabled() {
		a.problem(w, r, 403, "この画面からは名寄せアカウントを追加できません。")
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	if len(email) > 254 || !iap.ValidEmail(email) || strings.EqualFold(email, s.Email) {
		a.problem(w, r, 400, "追加するGoogleアカウントのメールアドレスを正しく入力してください。")
		return
	}
	token := store.RandomToken()
	if err := a.db.CreateInvite(r.Context(), s.User.ID, email, token, inviteTTL); err != nil {
		switch {
		case errors.Is(err, store.ErrLinkLimit):
			a.problem(w, r, 409, "名寄せできるアカウントは最大5個までです。")
		case errors.Is(err, store.ErrLinkRejected):
			a.problem(w, r, 409, "このアカウントはすでに追加されているか、追加できません。")
		default:
			a.fail(w, r, err)
		}
		return
	}
	// IAP rejects unlisted accounts before the app sees the link, so access is granted first.
	if !a.cfg.IAPAllowlist.Allows(email) {
		if err := a.granter.Grant(r.Context(), email); err != nil {
			a.db.DeleteInvite(r.Context(), token)
			a.fail(w, r, err)
			return
		}
	}
	link := a.cfg.BaseURL + "/accounts/link?token=" + url.QueryEscape(token)
	body := s.User.Name + " さんが、しおりであなたのGoogleアカウント（" + email + "）を名寄せアカウントとして追加しようとしています。\n\n" +
		"心当たりがある場合のみ、次のリンクを " + email + " でログインした状態で開き、確認画面で承認してください。\n" + link + "\n\n" +
		"・リンクの有効期限は24時間です。\n・アクセス権の反映に数分かかることがあります。開けない場合は少し待ってから再度お試しください。\n" +
		"・名寄せすると、このアカウントではベースアカウントのブックマークを操作します。\n心当たりがない場合は、このメールを無視してください。"
	if err := a.mailer.Send(r.Context(), email, "【しおり】アカウントの名寄せの確認", body); err != nil {
		a.db.DeleteInvite(r.Context(), token)
		if rerr := a.revokeUnused(r.Context(), email); rerr != nil {
			a.fail(w, r, rerr)
			return
		}
		a.fail(w, r, err)
		return
	}
	a.renderAccounts(w, r, 200, email+" に確認メールを送信しました。リンクを開いて承認すると名寄せが完了します。")
}

func (a *App) unlinkAccount(w http.ResponseWriter, r *http.Request) {
	s := session(r)
	login := r.FormValue("login")
	if !isBase(s) || !validID.MatchString(login) {
		a.problem(w, r, 403, "名寄せを解除できません。")
		return
	}
	link, err := a.db.Unlink(r.Context(), s.User.ID, login)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err = a.revokeUnused(r.Context(), link.Email); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings/accounts", http.StatusSeeOther)
}

func (a *App) linkConfirm(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	invite, ok := a.inviteFor(w, r, token)
	if !ok {
		return
	}
	name, err := a.db.UserName(r.Context(), invite.BaseID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Referrer-Policy", "same-origin")
	a.render(w, r, 200, Page{Title: "アカウントの名寄せ", AccountSettings: true, Accounts: &AccountsPage{Confirm: &invite, Token: token, BaseName: name}})
}

// inviteFor loads an invitation and checks it belongs to the signed-in email.
func (a *App) inviteFor(w http.ResponseWriter, r *http.Request, token string) (store.AccountInvite, bool) {
	if token == "" || len(token) > 128 {
		a.problem(w, r, 400, "名寄せのリンクが正しくありません。")
		return store.AccountInvite{}, false
	}
	invite, err := a.db.Invite(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		a.problem(w, r, 410, "名寄せのリンクが無効か、有効期限が切れています。ベースアカウントからもう一度招待してください。")
		return invite, false
	}
	if err != nil {
		a.fail(w, r, err)
		return invite, false
	}
	if !strings.EqualFold(session(r).Email, invite.Email) {
		a.problem(w, r, 403, "このリンクは別のGoogleアカウント宛てです。招待されたアカウントでログインして開いてください。")
		return invite, false
	}
	return invite, true
}

func (a *App) linkComplete(w http.ResponseWriter, r *http.Request) {
	s := session(r)
	token := r.FormValue("token")
	if _, ok := a.inviteFor(w, r, token); !ok {
		return
	}
	if _, err := a.db.ConfirmInvite(r.Context(), token, s.LoginID, s.Email); err != nil {
		switch {
		case errors.Is(err, store.ErrLinkLimit):
			a.problem(w, r, 409, "ベースアカウントの名寄せ上限（5個）に達しています。")
		case errors.Is(err, store.ErrLinkRejected):
			a.problem(w, r, 409, "このアカウントは名寄せできません（すでに名寄せ済み、または他のアカウントを名寄せしています）。")
		case errors.Is(err, store.ErrEmailMismatch):
			a.problem(w, r, 403, "このリンクは別のGoogleアカウント宛てです。")
		case errors.Is(err, store.ErrNotFound):
			a.problem(w, r, 410, "名寄せのリンクが無効か、有効期限が切れています。")
		default:
			a.fail(w, r, err)
		}
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
