package config

type Viewpoint struct {
	Name     string           `yaml:"name,omitempty" json:"name,omitempty"`
	Desc     string           `yaml:"desc,omitempty" json:"desc,omitempty"`
	Labels   []string         `yaml:"labels,omitempty" json:"labels,omitempty"`
	Tables   []string         `yaml:"tables,omitempty" json:"tables,omitempty"`
	Groups   []ViewpointGroup `yaml:"groups,omitempty" json:"groups,omitempty"`
	Distance int              `yaml:"distance,omitempty" json:"distance,omitempty"`
}

type ViewpointGroup struct {
	Name   string   `yaml:"name,omitempty" json:"name,omitempty"`
	Desc   string   `yaml:"desc,omitempty" json:"desc,omitempty"`
	Labels []string `yaml:"labels,omitempty" json:"labels,omitempty"`
	Tables []string `yaml:"tables,omitempty" json:"tables,omitempty"`
	Color  string   `yaml:"color,omitempty" json:"color,omitempty"`
}
