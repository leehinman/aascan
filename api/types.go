package api

type VersionsResponse struct {
	Versions []string `json:"versions"`
	Aliases  []string `json:"aliases"`
}

type VersionResponse struct {
	Version VersionDetail `json:"version"`
}

type VersionDetail struct {
	Version string  `json:"version"`
	Builds  []Build `json:"builds"`
}

type Build struct {
	Projects map[string]Project `json:"projects"`
}

type Project struct {
	Packages map[string]Package `json:"packages"`
}

type Package struct {
	URL          string            `json:"url"`
	SHAURL       string            `json:"sha_url"`
	ASCURL       string            `json:"asc_url"`
	Type         string            `json:"type"`
	Architecture string            `json:"architecture"`
	OS           []string          `json:"os"`
	Classifier   string            `json:"classifier"`
	Attributes   map[string]string `json:"attributes"`
	Name         string            `json:"-"`
}
