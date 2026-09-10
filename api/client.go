package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const baseURL = "https://artifacts-api.elastic.co/v1"

type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) GetVersions() ([]string, error) {
	var resp VersionsResponse
	if err := c.get("/versions", &resp); err != nil {
		return nil, err
	}
	return resp.Versions, nil
}

func (c *Client) GetProjects(version string) (map[string]map[string]Package, error) {
	var resp VersionResponse
	if err := c.get("/versions/"+version, &resp); err != nil {
		return nil, err
	}
	if len(resp.Version.Builds) == 0 {
		return nil, fmt.Errorf("no builds for version %s", version)
	}
	result := make(map[string]map[string]Package, len(resp.Version.Builds[0].Projects))
	for projectName, project := range resp.Version.Builds[0].Projects {
		pkgs := make(map[string]Package, len(project.Packages))
		for name, pkg := range project.Packages {
			pkg.Name = name
			pkgs[name] = pkg
		}
		result[projectName] = pkgs
	}
	return result, nil
}

func (c *Client) get(path string, out any) error {
	resp, err := c.http.Get(baseURL + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, path)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
