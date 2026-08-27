package jumpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListAssetsSendsJumpServerHeadersAndDecodesFlexibleLabels(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/perms/users/self/assets/" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-JMS-ORG"); got != "org-1" {
			t.Fatalf("X-JMS-ORG = %q", got)
		}
		if got := r.Header.Get("X-TZ"); got != "+08:00" {
			t.Fatalf("X-TZ = %q", got)
		}
		if got := r.Header.Get("Referer"); got != server.URL+"/" {
			t.Fatalf("Referer = %q", got)
		}
		query := r.URL.Query()
		if query.Get("oid") != "org-1" || query.Get("search") != "web" || query.Get("limit") != "50" {
			t.Fatalf("query = %#v", query)
		}
		_, _ = fmt.Fprint(w, `{"count":2,"results":[{"id":"a1","name":"web-1","address":"10.0.0.1","type":{"value":"linux","label":"Linux"},"category":{"value":"host","label":"Host"}},{"id":"a2","name":"web-2","address":"10.0.0.2","type":"linux","category":"host"}]}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "access-token", "org-1", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.Timezone = "+08:00"
	page, err := client.ListAssets(context.Background(), AssetQuery{Search: "web", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.Results[0].Type.Value != "linux" || page.Results[1].Category.Value != "host" {
		t.Fatalf("page = %#v", page)
	}
}

func TestGetAssetDecodesPermittedAccountsAndProtocols(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/perms/users/self/assets/asset-1/" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"id":"asset-1","name":"web","address":"10.0.0.1","permed_accounts":[{"id":"account-1","name":"root","username":"root"}],"permed_protocols":[{"name":"ssh","port":22}]}`)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "token", "", server.Client())

	asset, err := client.GetAsset(context.Background(), "asset-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(asset.Accounts) != 1 || asset.Accounts[0].Username != "root" || len(asset.Protocols) != 1 || asset.Protocols[0].Port != 22 {
		t.Fatalf("asset = %#v", asset)
	}
}

func TestCreateConnectionTokenUsesSSHClientContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/authentication/connection-token/" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var body ConnectionRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Asset != "asset-1" || body.Account != "account-1" || body.Protocol != "ssh" || body.ConnectMethod != "ssh_client" {
			t.Fatalf("body = %#v", body)
		}
		if body.ConnectOptions.TokenReusable || body.ConnectOptions.DisableAutoHash {
			t.Fatalf("options = %#v", body.ConnectOptions)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"id":"connection-1"}`)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "token", "org-1", server.Client())

	id, err := client.CreateConnectionToken(context.Background(), ConnectionRequest{Asset: "asset-1", Account: "account-1"})
	if err != nil || id != "connection-1" {
		t.Fatalf("CreateConnectionToken = %q, %v", id, err)
	}
}

func TestGetClientURLDecodesStrictSSHConnection(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"protocol": "ssh",
		"endpoint": map[string]any{"host": "gateway.example.test", "port": 2222},
		"token":    map[string]any{"id": "connection-1", "value": "connection-secret"},
	})
	deepLink := "jms://" + base64.StdEncoding.EncodeToString(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/authentication/connection-token/connection-1/client-url/" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"url": deepLink})
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "token", "", server.Client())

	connection, err := client.GetClientConnection(context.Background(), "connection-1")
	if err != nil {
		t.Fatal(err)
	}
	if connection.Endpoint.Host != "gateway.example.test" || connection.Endpoint.Port != 2222 || connection.Username() != "JMS-connection-1" || connection.Password() != "connection-secret" {
		t.Fatalf("connection = %#v", connection)
	}
}

func TestDecodeClientURLRejectsInvalidOrIncompletePayload(t *testing.T) {
	validJSON := func(value string) string {
		return "jms://" + base64.StdEncoding.EncodeToString([]byte(value))
	}
	for name, raw := range map[string]string{
		"wrong scheme":   "https://example.test",
		"bad base64":     "jms://%%not-base64",
		"wrong protocol": validJSON(`{"protocol":"rdp","endpoint":{"host":"gateway","port":22},"token":{"id":"id","value":"secret"}}`),
		"missing host":   validJSON(`{"protocol":"ssh","endpoint":{"port":22},"token":{"id":"id","value":"secret"}}`),
		"bad port":       validJSON(`{"protocol":"ssh","endpoint":{"host":"gateway","port":0},"token":{"id":"id","value":"secret"}}`),
		"missing token":  validJSON(`{"protocol":"ssh","endpoint":{"host":"gateway","port":22},"token":{"id":"id"}}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeClientURL(raw); err == nil {
				t.Fatal("DecodeClientURL unexpectedly succeeded")
			}
		})
	}
}

func TestAPIErrorDoesNotIncludeRequestSecretOrResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"detail":"server leaked connection-secret"}`)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "oauth-secret", "", server.Client())

	_, err := client.CreateConnectionToken(context.Background(), ConnectionRequest{Asset: "asset", InputSecret: "request-secret"})
	if err == nil {
		t.Fatal("request unexpectedly succeeded")
	}
	for _, secret := range []string{"connection-secret", "oauth-secret", "request-secret"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error exposed %q: %v", secret, err)
		}
	}
}

func TestListOrganizationsMergesPermissionScopesByID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/profile/permissions/" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"pam_orgs":[{"id":"o1","name":"One"}],"audit_orgs":[{"id":"o1","name":"One"},{"id":"o2","name":"Two"}],"console_orgs":[],"workbench_orgs":[{"id":"o3","name":"Three"}]}`)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "token", "", server.Client())

	organizations, err := client.ListOrganizations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(organizations) != 3 || organizations[0].ID != "o1" || organizations[2].ID != "o3" {
		t.Fatalf("organizations = %#v", organizations)
	}
}

func TestNewClientRejectsBaseURLWithUnexpectedComponents(t *testing.T) {
	for _, raw := range []string{"", "ftp://jump.example.test", "https://jump.example.test/path?token=secret"} {
		if _, err := NewClient(raw, "token", "", http.DefaultClient); err == nil {
			t.Fatalf("NewClient(%q) unexpectedly succeeded", raw)
		}
	}
}
