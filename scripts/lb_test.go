package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scriptSandbox copies scripts into a temp tree with fake gcloud, go and curl on PATH.
func scriptSandbox(t *testing.T, gcloud string, scripts ...string) (root, log string) {
	t.Helper()
	root = t.TempDir()
	for _, dir := range []string{"scripts", "bin"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range append([]string{"common.sh"}, scripts...) {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "scripts", name), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("domains: [data-cloud.jp]\ntags: [技術, AI]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(root, "bin", name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("gcloud", gcloud)
	write("go", "#!/usr/bin/env bash\nif [[ \"$3\" == --validate ]]; then exit 0; fi\ncp \"$4\" \"$5\"\n")
	write("curl", "#!/usr/bin/env bash\nprintf 'curl %s\\n' \"$*\" >> \"$GCLOUD_LOG\"\ncat >/dev/null <&0 2>/dev/null || true\nprintf '{\"bindings\":[{\"role\":\"roles/iap.httpsResourceAccessor\",\"members\":[\"domain:data-cloud.jp\"],\"condition\":{\"title\":\"shiori-linked-accounts\"}}]}'\n")
	return root, filepath.Join(root, "calls")
}

// cleanEnv drops deployment settings that a developer's shell or .env exports (make loads
// .env), so script tests behave the same on every machine.
func cleanEnv() []string {
	drop := []string{"LB_DOMAIN=", "IAP_OAUTH_CLIENT_ID=", "IAP_OAUTH_CLIENT_SECRET=", "DEPLOY_BASE_URL=", "MAIL_SENDER=", "PROJECT_ID=", "SERVICE=", "REGION="}
	var env []string
	for _, kv := range os.Environ() {
		keep := true
		for _, d := range drop {
			if strings.HasPrefix(kv, d) {
				keep = false
			}
		}
		if keep {
			env = append(env, kv)
		}
	}
	return env
}

func runScript(t *testing.T, root, log, script string, env ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(root, "scripts", script))
	cmd.Env = append(cleanEnv(), append([]string{"PATH=" + filepath.Join(root, "bin") + ":" + os.Getenv("PATH"), "GCLOUD_LOG=" + log, "PROJECT_ID=test-project", "SERVICE=shiori"}, env...)...)
	out, err := cmd.CombinedOutput()
	calls, _ := os.ReadFile(log)
	return string(calls), string(out), err
}

const lbGcloud = `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$GCLOUD_LOG"
if [[ "$1 $2" == 'projects describe' ]]; then echo 123456789; fi
if [[ "$2 $3" == 'addresses describe' && "$*" == *value* ]]; then echo 34.1.2.3; fi
if [[ "$2 $3" == 'ssl-certificates describe' && "$*" == *value* ]]; then echo PROVISIONING; fi
if [[ "$2 $3" == 'backend-services describe' && "$*" == *value* ]]; then echo 4455667788; fi
if [[ "$1 $2" == 'dns managed-zones' ]]; then echo 'uname-link uname.link.'; fi
if [[ "$1 $2 $3" == 'dns record-sets list' ]]; then exit 0; fi
if [[ "$1" == compute && "$3" == describe && "${EXISTING:-}" != 1 && "$*" != *value* ]]; then exit 1; fi
`

func TestLBRequiresDomainAndOAuthClient(t *testing.T) {
	root, log := scriptSandbox(t, lbGcloud, "lb.sh")
	if _, out, err := runScript(t, root, log, "lb.sh"); err == nil || !strings.Contains(out, "LB_DOMAIN") {
		t.Fatalf("missing domain accepted: %v %s", err, out)
	}
	calls, out, err := runScript(t, root, log, "lb.sh", "LB_DOMAIN=shiori.smc.uname.link")
	if err == nil || !strings.Contains(out, "OAuth consent screen") || strings.Contains(calls, "compute") {
		t.Fatalf("IAP without an OAuth client must stop before creating anything: %v\n%s\n%s", err, out, calls)
	}
}

func TestLBCreatesIAPProtectedBalancerAndDNS(t *testing.T) {
	root, log := scriptSandbox(t, lbGcloud, "lb.sh")
	calls, out, err := runScript(t, root, log, "lb.sh", "LB_DOMAIN=shiori.smc.uname.link", "IAP_OAUTH_CLIENT_ID=cid.apps.googleusercontent.com", "IAP_OAUTH_CLIENT_SECRET=sec")
	if err != nil {
		t.Fatalf("lb failed: %v\n%s\n%s", err, out, calls)
	}
	for _, want := range []string{
		"compute addresses create shiori-lb-ip --global",
		"network-endpoint-groups create shiori-neg --region=asia-northeast1 --network-endpoint-type=serverless --cloud-run-service=shiori",
		"backend-services create shiori-backend --global --load-balancing-scheme=EXTERNAL_MANAGED",
		"--iap=enabled,oauth2-client-id=cid.apps.googleusercontent.com,oauth2-client-secret=sec",
		"ssl-certificates create shiori-cert --global --domains=shiori.smc.uname.link",
		"network-endpoint-groups create shiori-api-neg --region=asia-northeast1 --network-endpoint-type=serverless --cloud-run-service=shiori-api",
		"backend-services create shiori-api-backend --global --load-balancing-scheme=EXTERNAL_MANAGED",
		"backend-services update shiori-api-backend --global --iap=disabled",
		"url-maps add-path-matcher shiori-urlmap --global --path-matcher-name=api --default-service=shiori-backend --path-rules=/api/*=shiori-api-backend --new-hosts=shiori.smc.uname.link",
		"forwarding-rules create shiori-https-rule",
		"--ports=443",
		"forwarding-rules create shiori-http-rule",
		"dns record-sets create shiori.smc.uname.link. --zone=uname-link",
		"--rrdatas=34.1.2.3",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing %q in calls:\n%s", want, calls)
		}
	}
	if !strings.Contains(out, "PROVISIONING") {
		t.Errorf("certificate state not reported: %s", out)
	}
}

func TestLBSkipsExistingResources(t *testing.T) {
	root, log := scriptSandbox(t, lbGcloud, "lb.sh")
	calls, out, err := runScript(t, root, log, "lb.sh", "EXISTING=1", "LB_DOMAIN=shiori.smc.uname.link", "IAP_OAUTH_CLIENT_ID=cid", "IAP_OAUTH_CLIENT_SECRET=sec")
	if err != nil {
		t.Fatalf("lb failed: %v\n%s", err, out)
	}
	for _, line := range strings.Split(calls, "\n") {
		if strings.Contains(line, " create ") && strings.Contains(line, "compute") {
			t.Errorf("existing resource recreated: %s", line)
		}
	}
}

func TestDeployWithLoadBalancerUsesBackendServiceIAP(t *testing.T) {
	root, log := scriptSandbox(t, lbGcloud, "deploy.sh")
	calls, out, err := runScript(t, root, log, "deploy.sh", "EXISTING=1", "LB_DOMAIN=shiori.smc.uname.link")
	if err != nil {
		t.Fatalf("deploy failed: %v\n%s\n%s", err, out, calls)
	}
	ui := strings.Index(calls, "run deploy shiori ")
	if ui < 0 {
		t.Fatalf("no UI deploy: %s", calls)
	}
	command := strings.Split(calls[ui:], "\n")[0]
	if !strings.Contains(calls, "API_BASE_URL=https://shiori.smc.uname.link") {
		t.Errorf("token page must show the domain's API endpoint: %s", calls)
	}
	for _, want := range []string{"--no-allow-unauthenticated --no-iap --ingress=internal-and-cloud-load-balancing", "IAP_AUDIENCE=/projects/123456789/global/backendServices/4455667788", "BASE_URL=https://shiori.smc.uname.link"} {
		if !strings.Contains(command, want) {
			t.Errorf("UI deploy missing %q: %s", want, command)
		}
	}
	if strings.Contains(command, "--allow-unauthenticated --") && !strings.Contains(command, "--no-allow-unauthenticated") {
		t.Errorf("UI must never be public: %s", command)
	}
	if !strings.Contains(calls, "iap_web/compute/services/4455667788:setIamPolicy") || !strings.Contains(calls, "iap_web/cloud_run-asia-northeast1/services/shiori:getIamPolicy") {
		t.Errorf("policy must move from the Cloud Run resource to the backend service: %s", calls)
	}
}

func TestDeployWithLoadBalancerNeedsTheBackendService(t *testing.T) {
	root, log := scriptSandbox(t, strings.Replace(lbGcloud, `echo 4455667788`, `true`, 1), "deploy.sh")
	calls, out, err := runScript(t, root, log, "deploy.sh", "EXISTING=1", "LB_DOMAIN=shiori.smc.uname.link")
	if err == nil || !strings.Contains(out, "make lb") || strings.Contains(calls, "run deploy") {
		t.Fatalf("deploy must stop before touching Cloud Run: %v\n%s\n%s", err, out, calls)
	}
}
