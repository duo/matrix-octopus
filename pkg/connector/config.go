package connector

import (
	"html/template"
	"strings"

	"gopkg.in/yaml.v3"

	_ "embed"

	up "go.mau.fi/util/configupgrade"
)

//go:embed example-config.yaml
var ExampleConfig string

// TODO:
type Config struct {
	ListenAddress string `yaml:"listen_address"`
	ListenSecret  string `yaml:"listen_secret"`

	DisplaynameTemplate string             `yaml:"displayname_template"`
	displaynameTemplate *template.Template `yaml:"-"`
}

type umConfig Config

func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	err := node.Decode((*umConfig)(c))
	if err != nil {
		return err
	}
	return c.PostProcess()
}

func (c *Config) PostProcess() error {
	var err error
	c.displaynameTemplate, err = template.New("displayname").Parse(c.DisplaynameTemplate)
	return err
}

func upgradeConfig(helper up.Helper) {
}

func (oc *OctopusConnector) GetConfig() (example string, data any, upgrader up.Upgrader) {
	return ExampleConfig, &oc.Config, up.SimpleUpgrader(upgradeConfig)
}

type DisplaynameParams struct {
	Alias string
	Name  string
	ID    string
}

func (c *Config) FormatDisplayname(params DisplaynameParams) string {
	var buffer strings.Builder
	_ = c.displaynameTemplate.Execute(&buffer, params)
	return buffer.String()
}
