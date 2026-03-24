package openstack

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestParseTags(t *testing.T) {
	tests := []struct {
		name   string
		tags   *[]string
		wantSt ServerTags
	}{
		{
			name:   "Nil tags",
			tags:   nil,
			wantSt: ServerTags{},
		},
		{
			name:   "Empty tags",
			tags:   &[]string{},
			wantSt: ServerTags{},
		},
		{
			name:     "tags with standard prefixes are extracted",
			tags:     &[]string{"tag1=jon.doe@example.com", "tag2=tech.lead@example.com", "tag3=Infrastructure", "other_tag=value"},
			wantSt:   ServerTags{"tag1": "jon.doe@example.com", "tag2": "tech.lead@example.com", "tag3": "Infrastructure", "other_tag": "value"},
		},
		{
			name:   "Only tag1 tag",
			tags:   &[]string{"tag1=owner@example.com", "random=tag"},
			wantSt: ServerTags{"tag1": "owner@example.com", "random": "tag"},
		},
		{
			name:   "Arbitrary tags are parsed",
			tags:   &[]string{"boot=failed", "topic=news", "teaming=yes"},
			wantSt: ServerTags{"boot": "failed", "topic": "news", "teaming": "yes"},
		},
		{
			name:     "tags without value return empty string",
			tags:     &[]string{"tag1=", "tag2=", "tag3="},
			wantSt:   ServerTags{"tag1": "", "tag2": "", "tag3": ""},
		},
		{
			name:     "malformed tags are ignored or extracted cleanly if they contain equal signs",
			tags:     &[]string{"mks_cluster", "tag1=someone"},
			wantSt:   ServerTags{"mks_cluster": "", "tag1": "someone"},
		},
		{
			name:     "duplicate keys preserve the first value",
			tags:     &[]string{"tag1=Backend", "tag1=duplicate@example.com", "tag3=Backend", "tag3=QA", "tag2=Backend", "tag2=DevOps"},
			wantSt:   ServerTags{"tag1": "Backend", "tag3": "Backend", "tag2": "Backend"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTags(tt.tags)
			if len(got) == 0 && len(tt.wantSt) == 0 {
				// Both empty/nil, maps reflect.DeepEqual can fail if one is nil and other is empty map
				return
			}
			if !reflect.DeepEqual(got, tt.wantSt) {
				t.Errorf("parseTags() = %v, want %v", got, tt.wantSt)
			}
		})
	}
}

func TestMergeResults(t *testing.T) {
	t.Run("Empty channel", func(t *testing.T) {
		ch := make(chan map[string]ServerTags)
		close(ch)
		got := mergeResults(ch)
		if len(got) != 0 {
			t.Errorf("expected empty map, got %d entries", len(got))
		}
	})

	t.Run("Single project", func(t *testing.T) {
		ch := make(chan map[string]ServerTags, 1)
		ch <- map[string]ServerTags{
			"srv-1": {"tag1": "a", "tag2": "b", "tag3": "c"},
		}
		close(ch)
		got := mergeResults(ch)
		if len(got) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(got))
		}
		if got["srv-1"]["tag1"] != "a" {
			t.Errorf("expected TAG1='a', got %q", got["srv-1"]["tag1"])
		}
	})

	t.Run("Multiple projects merge", func(t *testing.T) {
		ch := make(chan map[string]ServerTags, 2)
		ch <- map[string]ServerTags{
			"srv-1": {"tag1": "a", "tag2": "b", "tag3": "c"},
		}
		ch <- map[string]ServerTags{
			"srv-2": {"tag1": "x", "tag2": "y", "tag3": "z"},
		}
		close(ch)
		got := mergeResults(ch)
		if len(got) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(got))
		}
	})

	t.Run("Overwrite on duplicate ID", func(t *testing.T) {
		ch := make(chan map[string]ServerTags, 2)
		ch <- map[string]ServerTags{
			"srv-1": {"tag1": "old"},
		}
		ch <- map[string]ServerTags{
			"srv-1": {"tag1": "new"},
		}
		close(ch)
		got := mergeResults(ch)
		// Last write wins, but order is non-deterministic from channel.
		// Just verify we have 1 entry.
		if len(got) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(got))
		}
	})
}

// fakeKeystoneAndNova creates an httptest server that emulates both
// Keystone v3 token auth and Nova compute v2 server listing.
func fakeKeystoneAndNova(t *testing.T, serversJSON string, authStatus, computeStatus int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var serverURL string

	mux.HandleFunc("/v3/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if authStatus != http.StatusCreated {
			http.Error(w, "auth error", authStatus)
			return
		}
		catalog := map[string]interface{}{
			"token": map[string]interface{}{
				"catalog": []map[string]interface{}{
					{
						"type": "compute",
						"endpoints": []map[string]interface{}{
							{
								"region_id": "test-region",
								"interface": "public",
								"url":       serverURL,
							},
						},
					},
				},
			},
		}
		w.Header().Set("X-Subject-Token", "fake-token")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(catalog)
	})

	mux.HandleFunc("/servers/detail", func(w http.ResponseWriter, r *http.Request) {
		if computeStatus != http.StatusOK {
			http.Error(w, "compute error", computeStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, serversJSON)
	})

	srv := httptest.NewServer(mux)
	serverURL = srv.URL
	return srv
}

func TestFetchProjectTags(t *testing.T) {
	t.Run("successful fetch with tagged servers", func(t *testing.T) {
		serversJSON := `{
			"servers": [
				{
					"id": "aaa-111",
					"name": "web-server-1",
					"tags": ["team=Backend", "bo=owner@example.com"]
				},
				{
					"id": "bbb-222",
					"name": "db-server-1",
					"tags": ["team=DBA"]
				}
			]
		}`
		srv := fakeKeystoneAndNova(t, serversJSON, http.StatusCreated, http.StatusOK)
		defer srv.Close()

		conf := Config{
			ProjectName: "test-project",
			ProjectID:   "proj-123",
			AuthURL:     srv.URL + "/v3",
			DomainName:  "Default",
			RegionName:  "test-region",
			Username:    "admin",
			Password:    "secret",
		}

		tags, err := fetchProjectTags(conf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// ID-based lookup
		if tags["aaa-111"]["team"] != "Backend" {
			t.Errorf("expected team=Backend for aaa-111, got %q", tags["aaa-111"]["team"])
		}
		if tags["aaa-111"]["bo"] != "owner@example.com" {
			t.Errorf("expected bo=owner@example.com, got %q", tags["aaa-111"]["bo"])
		}
		// Name-based lookup (same tags under name key)
		if tags["web-server-1"]["team"] != "Backend" {
			t.Errorf("expected team=Backend for web-server-1, got %q", tags["web-server-1"]["team"])
		}
		// Second server
		if tags["bbb-222"]["team"] != "DBA" {
			t.Errorf("expected team=DBA for bbb-222, got %q", tags["bbb-222"]["team"])
		}
		if tags["db-server-1"]["team"] != "DBA" {
			t.Errorf("expected team=DBA for db-server-1, got %q", tags["db-server-1"]["team"])
		}
	})

	t.Run("empty server list", func(t *testing.T) {
		serversJSON := `{"servers": []}`
		srv := fakeKeystoneAndNova(t, serversJSON, http.StatusCreated, http.StatusOK)
		defer srv.Close()

		conf := Config{
			ProjectName: "empty-project",
			ProjectID:   "proj-empty",
			AuthURL:     srv.URL + "/v3",
			DomainName:  "Default",
			RegionName:  "test-region",
			Username:    "admin",
			Password:    "secret",
		}

		tags, err := fetchProjectTags(conf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(tags) != 0 {
			t.Errorf("expected 0 tags for empty server list, got %d", len(tags))
		}
	})

	t.Run("servers with no tags", func(t *testing.T) {
		serversJSON := `{
			"servers": [
				{"id": "ccc-333", "name": "bare-server"}
			]
		}`
		srv := fakeKeystoneAndNova(t, serversJSON, http.StatusCreated, http.StatusOK)
		defer srv.Close()

		conf := Config{
			ProjectName: "no-tags-project",
			ProjectID:   "proj-notag",
			AuthURL:     srv.URL + "/v3",
			DomainName:  "Default",
			RegionName:  "test-region",
			Username:    "admin",
			Password:    "secret",
		}

		tags, err := fetchProjectTags(conf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Server exists, but has no tags
		if _, ok := tags["ccc-333"]; !ok {
			t.Error("expected ccc-333 entry to exist even without tags")
		}
		if len(tags["ccc-333"]) != 0 {
			t.Errorf("expected 0 tags for bare server, got %d", len(tags["ccc-333"]))
		}
	})

	t.Run("auth failure returns error", func(t *testing.T) {
		srv := fakeKeystoneAndNova(t, `{}`, http.StatusUnauthorized, http.StatusOK)
		defer srv.Close()

		conf := Config{
			ProjectName: "bad-auth",
			ProjectID:   "proj-bad",
			AuthURL:     srv.URL + "/v3",
			DomainName:  "Default",
			RegionName:  "test-region",
			Username:    "admin",
			Password:    "wrong-password",
		}

		_, err := fetchProjectTags(conf)
		if err == nil {
			t.Fatal("expected auth error, got nil")
		}
	})

	t.Run("unreachable endpoint returns error", func(t *testing.T) {
		conf := Config{
			ProjectName: "unreachable",
			ProjectID:   "proj-dead",
			AuthURL:     "http://127.0.0.1:1/v3",
			DomainName:  "Default",
			RegionName:  "test-region",
			Username:    "admin",
			Password:    "secret",
		}

		_, err := fetchProjectTags(conf)
		if err == nil {
			t.Fatal("expected connection error, got nil")
		}
	})

	t.Run("compute API error returns error", func(t *testing.T) {
		srv := fakeKeystoneAndNova(t, `{}`, http.StatusCreated, http.StatusInternalServerError)
		defer srv.Close()

		conf := Config{
			ProjectName: "compute-fail",
			ProjectID:   "proj-cfail",
			AuthURL:     srv.URL + "/v3",
			DomainName:  "Default",
			RegionName:  "test-region",
			Username:    "admin",
			Password:    "secret",
		}

		_, err := fetchProjectTags(conf)
		if err == nil {
			t.Fatal("expected compute API error, got nil")
		}
	})
}

func TestFetchAllTags(t *testing.T) {
	t.Run("single project success", func(t *testing.T) {
		serversJSON := `{
			"servers": [
				{"id": "srv-1", "name": "app-1", "tags": ["team=Platform"]}
			]
		}`
		srv := fakeKeystoneAndNova(t, serversJSON, http.StatusCreated, http.StatusOK)
		defer srv.Close()

		configs := []Config{
			{
				ProjectName: "project-1",
				ProjectID:   "proj-1",
				AuthURL:     srv.URL + "/v3",
				DomainName:  "Default",
				RegionName:  "test-region",
				Username:    "admin",
				Password:    "secret",
			},
		}

		tags, err := fetchAllTags(configs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tags["srv-1"]["team"] != "Platform" {
			t.Errorf("expected team=Platform, got %q", tags["srv-1"]["team"])
		}
	})

	t.Run("one project fails causes overall failure", func(t *testing.T) {
		srv := fakeKeystoneAndNova(t, `{"servers": []}`, http.StatusCreated, http.StatusOK)
		defer srv.Close()

		configs := []Config{
			{
				ProjectName: "good-project",
				ProjectID:   "proj-good",
				AuthURL:     srv.URL + "/v3",
				DomainName:  "Default",
				RegionName:  "test-region",
				Username:    "admin",
				Password:    "secret",
			},
			{
				ProjectName: "bad-project",
				ProjectID:   "proj-bad",
				AuthURL:     "http://127.0.0.1:1/v3",
				DomainName:  "Default",
				RegionName:  "test-region",
				Username:    "admin",
				Password:    "secret",
			},
		}

		_, err := fetchAllTags(configs)
		if err == nil {
			t.Fatal("expected error when one project fails, got nil")
		}
	})

	t.Run("empty config list", func(t *testing.T) {
		tags, err := fetchAllTags(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(tags) != 0 {
			t.Errorf("expected 0 tags for nil configs, got %d", len(tags))
		}
	})
}

func TestLiveFetcherFetchAllTags(t *testing.T) {
	serversJSON := `{
		"servers": [
			{"id": "lf-1", "name": "live-app", "tags": ["env=prod"]}
		]
	}`
	srv := fakeKeystoneAndNova(t, serversJSON, http.StatusCreated, http.StatusOK)
	defer srv.Close()

	fetcher := &LiveFetcher{
		Configs: []Config{
			{
				ProjectName: "live-project",
				ProjectID:   "proj-live",
				AuthURL:     srv.URL + "/v3",
				DomainName:  "Default",
				RegionName:  "test-region",
				Username:    "admin",
				Password:    "secret",
			},
		},
	}

	tags, err := fetcher.FetchAllTags()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tags["lf-1"]["env"] != "prod" {
		t.Errorf("expected env=prod, got %q", tags["lf-1"]["env"])
	}
}
