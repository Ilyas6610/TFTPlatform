package riotapi_test

import (
	"context"
	"net/http"
	"testing"

	"tft-platform/internal/riotapi"
	"tft-platform/internal/riotapi/riotapitest"
)

func TestGetAccountByPUUID_UsesRoutingHostAndDecodes(t *testing.T) {
	var gotHost, gotPath string
	client := riotapitest.NewClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Header.Get(riotapitest.OriginalHostHeader)
		gotPath = r.URL.EscapedPath()
		w.Write([]byte(`{"puuid":"p/1","gameName":"Iron Bog","tagLine":"999"}`))
	}))

	account, err := client.GetAccountByPUUID(context.Background(), riotapi.RoutingEurope, "p/1")
	if err != nil {
		t.Fatalf("GetAccountByPUUID: %v", err)
	}
	if gotHost != "europe.api.riotgames.com" {
		t.Errorf("expected europe routing host, got %q", gotHost)
	}
	if want := "/riot/account/v1/accounts/by-puuid/p%2F1"; gotPath != want {
		t.Errorf("expected path %q (puuid escaped), got %q", want, gotPath)
	}
	if account.GameName != "Iron Bog" || account.TagLine != "999" {
		t.Errorf("unexpected account: %+v", account)
	}
}

func TestGetAccountByPUUID_404MapsToErrNotFound(t *testing.T) {
	client := riotapitest.NewClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	_, err := client.GetAccountByPUUID(context.Background(), riotapi.RoutingAmericas, "gone")
	if _, ok := err.(*riotapi.ErrNotFound); !ok {
		t.Fatalf("expected *ErrNotFound, got %T: %v", err, err)
	}
}

func TestAccountRoutingForPlatform(t *testing.T) {
	cases := map[riotapi.PlatformRegion]riotapi.RoutingRegion{
		riotapi.PlatformNA1:  riotapi.RoutingAmericas,
		riotapi.PlatformEUW1: riotapi.RoutingEurope,
		riotapi.PlatformKR:   riotapi.RoutingAsia,
		// account-v1 has no SEA cluster.
		riotapi.PlatformSG2: riotapi.RoutingAsia,
		riotapi.PlatformVN2: riotapi.RoutingAsia,
	}
	for platform, want := range cases {
		got, err := riotapi.AccountRoutingForPlatform(platform)
		if err != nil || got != want {
			t.Errorf("%s: expected %s, got %s (err %v)", platform, want, got, err)
		}
	}
	if _, err := riotapi.AccountRoutingForPlatform("nope"); err == nil {
		t.Error("expected error for unknown platform")
	}
}
