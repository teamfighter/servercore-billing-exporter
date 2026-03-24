package openstack

import (
	"fmt"
	"os"

	"gopkg.in/ini.v1"
)

// Config holds credentials for a single OpenStack project.
type Config struct {
	ProjectName string
	ProjectID   string
	AuthURL     string
	DomainName  string
	RegionName  string
	Username    string
	Password    string
}

// LoadConfig reads the OpenStack configurations from the specified INI file.
// It iterates through all sections (except DEFAULT) and builds a configuration array.
func LoadConfig(path string) ([]Config, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("openstack config file %s does not exist", path)
		}
		return nil, fmt.Errorf("error accessing config file %s: %w", path, err)
	}

	iniData, err := ini.LoadSources(ini.LoadOptions{
		IgnoreInlineComment: true,
	}, path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse INI config: %w", err)
	}

	var configs []Config
	for _, section := range iniData.Sections() {
		if section.Name() == "DEFAULT" {
			continue
		}

		configs = append(configs, Config{
			ProjectName: section.Name(),
			ProjectID:   section.Key("project_id").String(),
			AuthURL:     section.Key("auth_url").String(),
			DomainName:  section.Key("domain_name").String(),
			RegionName:  section.Key("region_name").String(),
			Username:    section.Key("username").String(),
			Password:    section.Key("password").String(),
		})
	}

	if len(configs) == 0 {
		return nil, fmt.Errorf("no openstack projects found in config %s", path)
	}

	return configs, nil
}
