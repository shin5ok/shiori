#!/usr/bin/env bash
# Creates the global external HTTPS load balancer in front of Cloud Run and enables IAP on it.
# Re-runnable: every resource is created only when missing.
source "$(dirname "$0")/common.sh"
LB_DOMAIN=${LB_DOMAIN:-}
IAP_OAUTH_CLIENT_ID=${IAP_OAUTH_CLIENT_ID:-}
IAP_OAUTH_CLIENT_SECRET=${IAP_OAUTH_CLIENT_SECRET:-}
[[ "$LB_DOMAIN" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?\.[a-z]{2,}$ ]] || { echo 'Set LB_DOMAIN (for example shiori.example.com) in .env.' >&2; exit 1; }
if [[ -z "$IAP_OAUTH_CLIENT_ID" || -z "$IAP_OAUTH_CLIENT_SECRET" ]]; then
  cat >&2 <<MSG
IAP behind a load balancer needs your own OAuth client (the Google-managed one serves only your organization).
1. APIs & Services > OAuth consent screen: User type "External", publish to "In production".
2. Credentials > Create OAuth client ID > Web application.
   Authorized redirect URI: https://iap.googleapis.com/v1/oauth/clientIds/<CLIENT_ID>:handleRedirect
3. Put IAP_OAUTH_CLIENT_ID and IAP_OAUTH_CLIENT_SECRET in .env and run make lb again.
MSG
  exit 1
fi
prefix=$SERVICE
ip_name="${prefix}-lb-ip"
neg="${prefix}-neg"
backend="${prefix}-backend"
urlmap="${prefix}-urlmap"
cert="${prefix}-cert"
api_neg="${prefix}-api-neg"
api_backend="${prefix}-api-backend"
https_proxy="${prefix}-https-proxy"
https_rule="${prefix}-https-rule"
redirect_map="${prefix}-redirect"
http_proxy="${prefix}-http-proxy"
http_rule="${prefix}-http-rule"
g() { gcloud compute "$@" --project="$PROJECT_ID"; }
exists() { gcloud compute "$@" --project="$PROJECT_ID" >/dev/null 2>&1; }

exists addresses describe "$ip_name" --global || g addresses create "$ip_name" --global --ip-version=IPV4
ip=$(g addresses describe "$ip_name" --global --format='value(address)')

exists network-endpoint-groups describe "$neg" --region="$REGION" || \
  g network-endpoint-groups create "$neg" --region="$REGION" --network-endpoint-type=serverless --cloud-run-service="$SERVICE"
if ! exists backend-services describe "$backend" --global; then
  g backend-services create "$backend" --global --load-balancing-scheme=EXTERNAL_MANAGED
  g backend-services add-backend "$backend" --global --network-endpoint-group="$neg" --network-endpoint-group-region="$REGION"
fi
g backend-services update "$backend" --global --iap="enabled,oauth2-client-id=$IAP_OAUTH_CLIENT_ID,oauth2-client-secret=$IAP_OAUTH_CLIENT_SECRET"

# /api/* skips IAP: the token API authenticates callers (curl, shortcuts) with its own bearer tokens.
exists network-endpoint-groups describe "$api_neg" --region="$REGION" || \
  g network-endpoint-groups create "$api_neg" --region="$REGION" --network-endpoint-type=serverless --cloud-run-service="${SERVICE}-api"
if ! exists backend-services describe "$api_backend" --global; then
  g backend-services create "$api_backend" --global --load-balancing-scheme=EXTERNAL_MANAGED
  g backend-services add-backend "$api_backend" --global --network-endpoint-group="$api_neg" --network-endpoint-group-region="$REGION"
fi
# Make sure IAP never ends up on the API backend.
g backend-services update "$api_backend" --global --iap=disabled

exists url-maps describe "$urlmap" --global || g url-maps create "$urlmap" --global --default-service="$backend"
if [[ -z $(g url-maps describe "$urlmap" --global --format='value(pathMatchers[].name)' | grep -Fx api || true) ]]; then
  g url-maps add-path-matcher "$urlmap" --global --path-matcher-name=api --default-service="$backend" \
    --path-rules="/api/*=$api_backend" --new-hosts="$LB_DOMAIN"
fi
exists ssl-certificates describe "$cert" --global || g ssl-certificates create "$cert" --global --domains="$LB_DOMAIN"
exists target-https-proxies describe "$https_proxy" --global || \
  g target-https-proxies create "$https_proxy" --global --url-map="$urlmap" --ssl-certificates="$cert"
exists forwarding-rules describe "$https_rule" --global || \
  g forwarding-rules create "$https_rule" --global --load-balancing-scheme=EXTERNAL_MANAGED --network-tier=PREMIUM --address="$ip_name" --target-https-proxy="$https_proxy" --ports=443

# Plain HTTP only redirects to HTTPS.
if ! exists url-maps describe "$redirect_map" --global; then
  redirect_file=$(mktemp)
  trap 'rm -f "$redirect_file"' EXIT
  printf 'name: %s\ndefaultUrlRedirect:\n  redirectResponseCode: MOVED_PERMANENTLY_DEFAULT\n  httpsRedirect: true\n' "$redirect_map" > "$redirect_file"
  g url-maps import "$redirect_map" --global --source="$redirect_file" --quiet
fi
exists target-http-proxies describe "$http_proxy" --global || g target-http-proxies create "$http_proxy" --global --url-map="$redirect_map"
exists forwarding-rules describe "$http_rule" --global || \
  g forwarding-rules create "$http_rule" --global --load-balancing-scheme=EXTERNAL_MANAGED --network-tier=PREMIUM --address="$ip_name" --target-http-proxy="$http_proxy" --ports=80

# Point the domain at the load balancer when its zone lives in this project's Cloud DNS.
zone=""
while read -r name dns; do
  [[ -n "$name" && ".$LB_DOMAIN." == *".${dns%.}."* ]] && { zone=$name; break; }
done < <(gcloud dns managed-zones list --project="$PROJECT_ID" --filter='visibility=public' --format='value(name,dnsName)' 2>/dev/null || true)
if [[ -n "$zone" ]]; then
  existing=$(gcloud dns record-sets list --zone="$zone" --project="$PROJECT_ID" --name="$LB_DOMAIN." --type=A --format='value(rrdatas[0])' 2>/dev/null || true)
  if [[ -z "$existing" ]]; then
    gcloud dns record-sets create "$LB_DOMAIN." --zone="$zone" --project="$PROJECT_ID" --type=A --ttl=300 --rrdatas="$ip"
  elif [[ "$existing" != "$ip" ]]; then
    gcloud dns record-sets update "$LB_DOMAIN." --zone="$zone" --project="$PROJECT_ID" --type=A --ttl=300 --rrdatas="$ip"
  fi
  echo "DNS: $LB_DOMAIN -> $ip (Cloud DNS zone $zone)"
else
  echo "DNS: add this record where $LB_DOMAIN is managed:  $LB_DOMAIN.  A  $ip"
fi
status=$(g ssl-certificates describe "$cert" --global --format='value(managed.status)')
echo "Certificate: ${status:-unknown} (becomes ACTIVE once DNS resolves to $ip; this can take tens of minutes)"
cat <<MSG

Next:
1. Wait until the certificate is ACTIVE, then open https://$LB_DOMAIN and confirm the Google sign-in appears.
2. Set LB_DOMAIN=$LB_DOMAIN in .env and run make deploy. Cloud Run then stops serving its run.app URL and accepts only this load balancer.
MSG
