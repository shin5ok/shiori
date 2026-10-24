package iap

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testBinding struct {
	Role      string   `json:"role"`
	Members   []string `json:"members"`
	Condition *struct {
		Title string `json:"title"`
	} `json:"condition"`
}

func bindingsOf(t *testing.T, data []byte) (int, []testBinding) {
	t.Helper()
	var policy struct {
		Version  int           `json:"version"`
		Bindings []testBinding `json:"bindings"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	return policy.Version, policy.Bindings
}

func TestSetLinkedMemberManagesOnlyItsBinding(t *testing.T) {
	policy := []byte(`{"etag":"abc","bindings":[{"role":"roles/iap.httpsResourceAccessor","members":["domain:data-cloud.jp"]}]}`)
	added, err := SetLinkedMember(policy, "Guest@Example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	version, bindings := bindingsOf(t, added)
	if version != 3 || len(bindings) != 2 || bindings[0].Members[0] != "domain:data-cloud.jp" ||
		bindings[1].Condition == nil || bindings[1].Condition.Title != LinkedBindingTitle || bindings[1].Members[0] != "user:guest@example.com" {
		t.Fatalf("add: %s", added)
	}
	if !strings.Contains(string(added), `"etag": "abc"`) {
		t.Fatalf("etag lost: %s", added)
	}
	again, _ := SetLinkedMember(added, "guest@example.com", true)
	if _, b := bindingsOf(t, again); len(b) != 2 || len(b[1].Members) != 1 || b[1].Members[0] != "user:guest@example.com" {
		t.Fatalf("duplicate member: %s", again)
	}
	removed, err := SetLinkedMember(added, "guest@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, b := bindingsOf(t, removed); len(b) != 1 || b[0].Condition != nil {
		t.Fatalf("remove: %s", removed)
	}
	if _, err = SetLinkedMember(policy, "bad address", true); err == nil {
		t.Fatal("invalid email accepted")
	}
}

func TestReconcilePolicyKeepsLinkedAccounts(t *testing.T) {
	list, err := NewAllowlist([]string{"new@data-cloud.jp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	policy, _ := SetLinkedMember([]byte(`{"bindings":[{"role":"roles/iap.httpsResourceAccessor","members":["user:old@data-cloud.jp"]}]}`), "guest@example.com", true)
	out, err := ReconcilePolicy(policy, list)
	if err != nil {
		t.Fatal(err)
	}
	version, bindings := bindingsOf(t, out)
	var plain, linked []string
	for _, b := range bindings {
		if b.Condition != nil {
			linked = append(linked, b.Members...)
		} else {
			plain = append(plain, b.Members...)
		}
	}
	if version != 3 || strings.Join(plain, ",") != "user:new@data-cloud.jp" || strings.Join(linked, ",") != "user:guest@example.com" {
		t.Fatalf("version=%d plain=%v linked=%v", version, plain, linked)
	}
}

func TestGrantsReadsVersion3AndRetriesEtagConflicts(t *testing.T) {
	var sets int
	var lastSet string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(r.URL.Path, ":getIamPolicy"):
			if !strings.Contains(string(body), `"requestedPolicyVersion":3`) {
				t.Errorf("policy read without version 3: %s", body)
			}
			w.Write([]byte(`{"etag":"e","bindings":[]}`))
		case strings.HasSuffix(r.URL.Path, ":setIamPolicy"):
			sets++
			if sets == 1 {
				http.Error(w, "etag", http.StatusConflict)
				return
			}
			lastSet = string(body)
			w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()
	g, err := NewGrants(server.Client(), "/projects/123/locations/asia-northeast1/services/shiori")
	if err != nil {
		t.Fatal(err)
	}
	g.Endpoint = server.URL
	if err = g.Grant(context.Background(), "guest@example.com"); err != nil {
		t.Fatal(err)
	}
	if sets != 2 || !strings.Contains(lastSet, "user:guest@example.com") {
		t.Fatalf("sets=%d body=%s", sets, lastSet)
	}
	if _, err = NewGrants(server.Client(), "/projects/x"); err == nil {
		t.Fatal("bad audience accepted")
	}
}
