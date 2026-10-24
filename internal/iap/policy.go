package iap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var emailPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._%+\-]*@[a-z0-9][a-z0-9.\-]*\.[a-z]{2,}$`)
var domainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+$`)

type Allowlist struct {
	accounts map[string]bool
	domains  map[string]bool
}

func ParseAllowlist(data []byte) (Allowlist, error) {
	if len(data) > 64*1024 {
		return Allowlist{}, errors.New("config.yaml is too large")
	}
	var raw struct {
		Accounts []string `yaml:"accounts"`
		Domains  []string `yaml:"domains"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		return Allowlist{}, fmt.Errorf("parse config.yaml: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Allowlist{}, errors.New("config.yaml must contain one document")
	}
	return NewAllowlist(raw.Accounts, raw.Domains)
}

// NewAllowlist validates and normalizes the configured account rules.
func NewAllowlist(accounts, domains []string) (Allowlist, error) {
	list := Allowlist{accounts: map[string]bool{}, domains: map[string]bool{}}
	for _, value := range accounts {
		value = strings.ToLower(strings.TrimSpace(value))
		if !emailPattern.MatchString(value) || list.accounts[value] {
			return Allowlist{}, fmt.Errorf("invalid or duplicate account %q", value)
		}
		list.accounts[value] = true
	}
	for _, value := range domains {
		value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "@")
		if !domainPattern.MatchString(value) || list.domains[value] {
			return Allowlist{}, fmt.Errorf("invalid or duplicate domain %q", value)
		}
		list.domains[value] = true
	}
	if len(list.accounts)+len(list.domains) == 0 {
		return Allowlist{}, errors.New("config.yaml must allow at least one account or domain")
	}
	return list, nil
}

func (l Allowlist) Allows(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if !emailPattern.MatchString(email) {
		return false
	}
	if l.accounts[email] {
		return true
	}
	_, domain, _ := strings.Cut(email, "@")
	return l.domains[domain]
}

func (l Allowlist) Members() []string {
	members := make([]string, 0, len(l.accounts)+len(l.domains))
	for email := range l.accounts {
		members = append(members, "user:"+email)
	}
	for domain := range l.domains {
		members = append(members, "domain:"+domain)
	}
	sort.Strings(members)
	return members
}

func ReconcilePolicy(existing []byte, list Allowlist) ([]byte, error) {
	var policy map[string]json.RawMessage
	if err := json.Unmarshal(existing, &policy); err != nil {
		return nil, fmt.Errorf("parse IAP IAM policy: %w", err)
	}
	if policy == nil {
		return nil, errors.New("empty IAP IAM policy")
	}
	var bindings []map[string]json.RawMessage
	if raw := policy["bindings"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &bindings); err != nil {
			return nil, fmt.Errorf("parse IAP IAM bindings: %w", err)
		}
	}
	kept := make([]map[string]json.RawMessage, 0, len(bindings)+1)
	for _, binding := range bindings {
		role, err := bindingRole(binding)
		if err != nil {
			return nil, err
		}
		// Accounts linked at runtime live in their own binding; deploys must not revoke them.
		if role != accessorRole || isLinkedBinding(binding) {
			kept = append(kept, binding)
		}
	}
	members := list.Members()
	if len(members) == 0 {
		return nil, errors.New("empty IAP allowlist")
	}
	memberData, _ := json.Marshal(members)
	kept = append(kept, map[string]json.RawMessage{
		"role":    json.RawMessage(`"roles/iap.httpsResourceAccessor"`),
		"members": memberData,
	})
	bindingsData, err := json.Marshal(kept)
	if err != nil {
		return nil, err
	}
	policy["bindings"] = bindingsData
	setConditionalVersion(policy, kept)
	return json.MarshalIndent(policy, "", "  ")
}

const accessorRole = "roles/iap.httpsResourceAccessor"

// LinkedBindingTitle names the conditional binding the application manages for linked accounts.
const LinkedBindingTitle = "shiori-linked-accounts"

// IAP bindings need a condition to be identifiable; this one never expires in practice.
const linkedExpression = `request.time < timestamp("2099-01-01T00:00:00Z")`

// ValidEmail reports whether value is an acceptable lowercase email address.
func ValidEmail(value string) bool { return emailPattern.MatchString(value) }

func bindingRole(binding map[string]json.RawMessage) (string, error) {
	var role string
	if err := json.Unmarshal(binding["role"], &role); err != nil {
		return "", fmt.Errorf("parse IAP IAM role: %w", err)
	}
	return role, nil
}

func isLinkedBinding(binding map[string]json.RawMessage) bool {
	var condition struct {
		Title string `json:"title"`
	}
	return json.Unmarshal(binding["condition"], &condition) == nil && condition.Title == LinkedBindingTitle
}

// Conditional bindings are only honoured by version 3 policies.
func setConditionalVersion(policy map[string]json.RawMessage, bindings []map[string]json.RawMessage) {
	for _, binding := range bindings {
		if len(binding["condition"]) > 0 {
			policy["version"] = json.RawMessage("3")
			return
		}
	}
}

// SetLinkedMember adds or removes user:email in the application-managed binding.
// The policy must have been read with requestedPolicyVersion 3 so conditions survive.
func SetLinkedMember(existing []byte, email string, add bool) ([]byte, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !emailPattern.MatchString(email) {
		return nil, fmt.Errorf("invalid account %q", email)
	}
	var policy map[string]json.RawMessage
	if err := json.Unmarshal(existing, &policy); err != nil || policy == nil {
		return nil, fmt.Errorf("parse IAP IAM policy: %v", err)
	}
	var bindings []map[string]json.RawMessage
	if raw := policy["bindings"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &bindings); err != nil {
			return nil, fmt.Errorf("parse IAP IAM bindings: %w", err)
		}
	}
	member := "user:" + email
	var members []string
	kept := make([]map[string]json.RawMessage, 0, len(bindings)+1)
	for _, binding := range bindings {
		role, err := bindingRole(binding)
		if err != nil {
			return nil, err
		}
		if role == accessorRole && isLinkedBinding(binding) {
			if err := json.Unmarshal(binding["members"], &members); err != nil {
				return nil, fmt.Errorf("parse linked IAP members: %w", err)
			}
			continue
		}
		kept = append(kept, binding)
	}
	next := make([]string, 0, len(members)+1)
	for _, m := range members {
		if m != member {
			next = append(next, m)
		}
	}
	if add {
		next = append(next, member)
	}
	sort.Strings(next)
	if len(next) > 0 {
		membersData, _ := json.Marshal(next)
		conditionData, _ := json.Marshal(map[string]string{"title": LinkedBindingTitle, "expression": linkedExpression})
		kept = append(kept, map[string]json.RawMessage{
			"role":      json.RawMessage(`"` + accessorRole + `"`),
			"members":   membersData,
			"condition": conditionData,
		})
	}
	bindingsData, err := json.Marshal(kept)
	if err != nil {
		return nil, err
	}
	policy["bindings"] = bindingsData
	setConditionalVersion(policy, kept)
	return json.MarshalIndent(policy, "", "  ")
}
