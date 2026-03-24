// Package openstack provides OpenStack Compute API integration for fetching VM tags.
package openstack

import (
	"fmt"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"golang.org/x/sync/errgroup"
)

// ServerTags holds key-value labels extracted from OpenStack VM tags.
type ServerTags map[string]string

// TagFetcher abstracts OpenStack tag retrieval for testability.
type TagFetcher interface {
	FetchAllTags() (map[string]ServerTags, error)
}

// LiveFetcher implements TagFetcher using real OpenStack API calls.
type LiveFetcher struct {
	Configs []Config
}

// FetchAllTags fetches tags from all configured projects.
func (f *LiveFetcher) FetchAllTags() (map[string]ServerTags, error) {
	return fetchAllTags(f.Configs)
}

// fetchAllTags queries all configured OpenStack projects concurrently
// and returns a combined map of server ID -> ServerTags.
func fetchAllTags(configs []Config) (map[string]ServerTags, error) {
	var g errgroup.Group

	results := make(chan map[string]ServerTags, len(configs))

	for _, conf := range configs {
		conf := conf // capture loop variable
		g.Go(func() error {
			tags, err := fetchProjectTags(conf)
			if err != nil {
				return err
			}
			results <- tags
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	close(results)

	return mergeResults(results), nil
}

// fetchProjectTags authenticates against a single OpenStack project
// and returns a map of server ID -> ServerTags for that project.
func fetchProjectTags(conf Config) (map[string]ServerTags, error) {
	provider, err := openstack.AuthenticatedClient(gophercloud.AuthOptions{
		IdentityEndpoint: conf.AuthURL,
		TenantID:         conf.ProjectID,
		DomainName:       conf.DomainName,
		Username:         conf.Username,
		Password:         conf.Password,
	})
	if err != nil {
		return nil, fmt.Errorf("auth failed for %s: %w", conf.ProjectName, err)
	}

	client, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: conf.RegionName,
	})
	if err != nil {
		return nil, fmt.Errorf("compute client failed for %s: %w", conf.ProjectName, err)
	}

	// Microversion 2.26 is required for server tags.
	client.Microversion = "2.26"

	allPages, err := servers.List(client, servers.ListOpts{}).AllPages()
	if err != nil {
		return nil, fmt.Errorf("list servers failed for %s: %w", conf.ProjectName, err)
	}

	srvs, err := servers.ExtractServers(allPages)
	if err != nil {
		return nil, fmt.Errorf("extract servers failed for %s: %w", conf.ProjectName, err)
	}

	tags := make(map[string]ServerTags, len(srvs)*2)
	for _, srv := range srvs {
		parsed := parseTags(srv.Tags)
		tags[srv.ID] = parsed
		if srv.Name != "" {
			tags[srv.Name] = parsed
		}
	}

	return tags, nil
}

// mergeResults combines per-project tag maps into a single global map.
func mergeResults(results <-chan map[string]ServerTags) map[string]ServerTags {
	merged := make(map[string]ServerTags)
	for projectTags := range results {
		for id, tags := range projectTags {
			merged[id] = tags
		}
	}
	return merged
}

// parseTags extracts key-value pairs from OpenStack VM tags.
// If multiple tags share the same key, the first value wins.
func parseTags(tags *[]string) ServerTags {
	st := make(ServerTags)
	if tags == nil {
		return st
	}
	for _, tag := range *tags {
		parts := strings.SplitN(tag, "=", 2)
		key := parts[0]
		if _, exists := st[key]; !exists {
			if len(parts) == 2 {
				st[key] = parts[1]
			} else {
				st[key] = ""
			}
		}
	}
	return st
}
