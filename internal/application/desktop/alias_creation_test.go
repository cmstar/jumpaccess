package desktop

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	settingsapp "github.com/cmstar/jumpaccess/internal/application/settings"
	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

type synchronizedAliasResources struct {
	fakeResources
	entered chan struct{}
	release chan struct{}
}

func (r *synchronizedAliasResources) FindAsset(_ context.Context, _, _, reference string) (jumpserver.AssetDetail, error) {
	r.entered <- struct{}{}
	<-r.release
	return jumpserver.AssetDetail{Asset: jumpserver.Asset{ID: reference}}, nil
}

func TestConcurrentAliasCreationDoesNotOverwriteWinner(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	settings := settingsapp.Service{Store: store}
	if err := settings.AddProfile("work", "https://jump.example.test"); err != nil {
		t.Fatal(err)
	}
	resources := &synchronizedAliasResources{entered: make(chan struct{}, 2), release: make(chan struct{})}
	defer func() {
		select {
		case <-resources.release:
		default:
			close(resources.release)
		}
	}()
	type result struct {
		asset string
		err   error
	}
	results := make(chan result, 2)
	for _, asset := range []string{"asset-one", "asset-two"} {
		go func() {
			service := Service{Config: store, Resources: resources, Settings: settingsapp.Service{Store: store}}
			_, err := service.CreateAlias(context.Background(), CreateAliasRequest{Profile: "work", Asset: asset, Name: "production"})
			results <- result{asset: asset, err: err}
		}()
	}
	for range 2 {
		select {
		case <-resources.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("creation did not reach resource lookup")
		}
	}
	close(resources.release)
	successful, winner := 0, ""
	for range 2 {
		got := <-results
		if got.err == nil {
			successful++
			winner = got.asset
		}
	}
	if successful != 1 {
		t.Fatalf("successful creations = %d, want exactly one", successful)
	}
	configuration, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Profiles["work"].Aliases["production"].Asset != winner {
		t.Fatal("duplicate creation overwrote winner")
	}
}
