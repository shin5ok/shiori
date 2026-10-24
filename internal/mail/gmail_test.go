package mail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGmailSendsEncodedMessage(t *testing.T) {
	var raw string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		json.Unmarshal(body, &payload)
		raw = payload["raw"]
		if r.URL.Path != "/gmail/v1/users/me/messages/send" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	g := &Gmail{Client: server.Client(), Endpoint: server.URL, Sender: "noreply@data-cloud.jp"}
	if err := g.Send(context.Background(), "guest@example.com", "確認", "本文のリンク"); err != nil {
		t.Fatal(err)
	}
	msg, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || !strings.Contains(string(msg), "To: guest@example.com") || !strings.Contains(string(msg), "From: noreply@data-cloud.jp") {
		t.Fatalf("message: %v %q", err, msg)
	}
	if err := g.Send(context.Background(), "a@b.jp\r\nBcc: x@y.jp", "s", "b"); err == nil {
		t.Fatal("header injection accepted")
	}
}
