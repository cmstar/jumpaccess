package agentmcp

import (
	"context"
	"testing"

	"github.com/cmstar/jumpaccess/internal/application/auth"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
)

type connectorIdentity struct{ login string }

func (i *connectorIdentity) Identity(context.Context, string) (auth.LoginIdentity, error) {
	return auth.LoginIdentity{Site: "https://agent.example.test", LoginID: i.login}, nil
}
func (i *connectorIdentity) EnsureFresh(context.Context, string) (credential.Token, error) {
	return credential.Token{Site: "https://agent.example.test", LoginID: i.login}, nil
}

func TestConnectorFreezesIdentityAndRejectsAmbiguousAlias(t *testing.T) {
	q, s, _ := queryFixture(t)
	identity := &connectorIdentity{login: "one"}
	c := Connector{Config: s.Store, Queries: q, Auth: identity}
	ctx := context.Background()
	for _, target := range []Target{{Profile: "agent", Alias: "ambiguous"}, {Profile: "agent", AssetID: "host-name", AccountID: "a", Organization: "org"}, {Profile: "agent", AssetID: "00000000-0000-4000-8000-000000000001", AccountID: "a"}} {
		if _, err := c.Resolve(ctx, target); err == nil {
			t.Fatal("accepted ambiguous or incomplete target")
		}
	}
	target, err := c.Resolve(ctx, Target{Profile: "agent", Organization: "org", AssetID: "00000000-0000-4000-8000-000000000001", AccountID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Validate(ctx, target); err != nil {
		t.Fatal(err)
	}
	identity.login = "two"
	assertFault(t, c.Validate(ctx, target), "IDENTITY_CHANGED")
	_, err = (boundTokens{source: identity, target: target}).EnsureFresh(ctx, "agent")
	assertFault(t, err, "IDENTITY_CHANGED")
	identity.login = "one"
	if err := s.Store.Update(ctx, func(cfg *config.Config) error {
		p := cfg.Profiles["agent"]
		p.URL = "https://changed.example.test"
		cfg.Profiles["agent"] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assertFault(t, c.Validate(ctx, target), "IDENTITY_CHANGED")
	cfg, _ := s.Store.Load()
	if cfg.CurrentProfile != "human" {
		t.Fatal("changed human profile")
	}
}
