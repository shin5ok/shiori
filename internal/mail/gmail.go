// Package mail sends account-linking notifications.
package mail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/mail"
	"strings"

	"golang.org/x/oauth2"
	"google.golang.org/api/impersonate"
)

// Gmail sends as a Workspace user through domain-wide delegation. The runtime
// service account signs the delegation assertion keylessly, so it needs the
// Service Account Token Creator role on itself.
type Gmail struct {
	Client   *http.Client
	Endpoint string
	Sender   string
}

// NewGmail builds a sender that impersonates sender via serviceAccount.
func NewGmail(ctx context.Context, serviceAccount, sender string) (*Gmail, error) {
	if _, err := mail.ParseAddress(sender); err != nil {
		return nil, fmt.Errorf("invalid MAIL_SENDER: %w", err)
	}
	source, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
		TargetPrincipal: serviceAccount,
		Scopes:          []string{"https://www.googleapis.com/auth/gmail.send"},
		Subject:         sender,
	})
	if err != nil {
		return nil, err
	}
	return &Gmail{Client: oauth2.NewClient(ctx, source), Endpoint: "https://gmail.googleapis.com", Sender: sender}, nil
}

func (g *Gmail) Send(ctx context.Context, to, subject, body string) error {
	if err := Validate(g.Sender, to, subject); err != nil {
		return err
	}
	raw := base64.RawURLEncoding.EncodeToString(message(g.Sender, to, subject, body))
	payload, _ := json.Marshal(map[string]string{"raw": raw})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(g.Endpoint, "/")+"/gmail/v1/users/me/messages/send", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.Client.Do(req)
	if err != nil {
		return fmt.Errorf("send mail: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("send mail: Gmail returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func message(from, to, subject, body string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\n", from, to, mime.BEncoding.Encode("utf-8", subject))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
	enc := base64.StdEncoding.EncodeToString([]byte(body))
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	return b.Bytes()
}

// Log prints messages instead of sending them; for local development only.
type Log struct{}

func (Log) Send(_ context.Context, to, subject, body string) error {
	slog.Info("mail (not sent)", "to", to, "subject", subject, "body", body)
	return nil
}

// Validate rejects header-injection characters in addresses and subjects.
func Validate(values ...string) error {
	for _, v := range values {
		if strings.ContainsAny(v, "\r\n") {
			return errors.New("invalid mail header value")
		}
	}
	return nil
}
