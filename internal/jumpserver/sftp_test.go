package jumpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateSFTPConnectionTokenUsesSFTPClientContract(t *testing.T) {
	var request ConnectionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":"sftp-connection"}`)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "access", "org", server.Client())
	_, err := client.CreateConnectionToken(context.Background(), ConnectionRequest{Asset: "asset", Account: "account", Protocol: "sftp"})
	if err != nil {
		t.Fatal(err)
	}
	if request.Protocol != "sftp" || request.ConnectMethod != "sftp_client" {
		t.Fatalf("request protocol/method = %q/%q", request.Protocol, request.ConnectMethod)
	}
	if request.ConnectOptions.TokenReusable || request.ConnectOptions.DisableAutoHash {
		t.Fatal("unsafe reusable connection options")
	}
}

func TestDecodeClientURLAcceptsSFTPGatewayCredential(t *testing.T) {
	raw := "jms://" + base64.StdEncoding.EncodeToString([]byte(`{"protocol":"sftp","endpoint":{"host":"gateway.test","port":"2222"},"token":{"id":"connection","value":"test-password"}}`))
	connection, err := DecodeClientURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	if connection.Protocol != "sftp" || connection.Endpoint.Port != 2222 || connection.Username() != "JMS-connection" {
		t.Fatalf("unexpected connection protocol/endpoint")
	}
}

func TestConnectionTokenRejectsUnsupportedProtocolBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":"id"}`)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "access", "org", server.Client())
	_, err := client.CreateConnectionToken(context.Background(), ConnectionRequest{Asset: "asset", Protocol: "rdp"})
	if err == nil || requests != 0 {
		t.Fatalf("unsupported protocol error=%v requests=%d", err, requests)
	}
}

func TestAssetMetadataPreservesSFTPRootAndExplicitEmptyPermissions(t *testing.T) {
	var detail AssetDetail
	if err := json.Unmarshal([]byte(`{"permed_protocols":[{"name":"sftp","port":22,"setting":{"sftp_home":"/srv/${ACCOUNT}"}}],"permed_accounts":[{"id":"readonly","actions":[{"value":"download","label":"Download"}]},{"id":"denied","actions":[]},{"id":"unknown"}]}`), &detail); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	var actual struct {
		Protocols []struct {
			Setting map[string]string `json:"setting"`
		} `json:"permed_protocols"`
		Accounts []struct {
			Actions []LabelValue `json:"actions"`
		} `json:"permed_accounts"`
	}
	if err := json.Unmarshal(encoded, &actual); err != nil {
		t.Fatal(err)
	}
	if actual.Protocols[0].Setting["sftp_home"] != "/srv/${ACCOUNT}" {
		t.Fatal("lost server SFTP root mapping")
	}
	if len(actual.Accounts[0].Actions) != 1 || actual.Accounts[1].Actions == nil || actual.Accounts[2].Actions != nil {
		t.Fatal("lost explicit/unknown account permission distinction")
	}
}
