package triton

import (
	"time"

	"github.com/hashicorp/nomad/plugins/base"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/hashicorp/nomad/plugins/shared/hclspec"
)

const (
	// pluginName is the name of the plugin
	// this is used for logging and (along with the version) for uniquely
	// identifying plugin binaries fingerprinted by the client
	pluginName = "triton"

	// pluginVersion allows the client to identify and use newer versions of
	// an installed plugin
	pluginVersion = "v0.1.0"

	// fingerprintPeriod is the interval at which the plugin will send
	// fingerprint responses
	fingerprintPeriod = 30 * time.Second

	// taskHandleVersion is the version of task handle which this plugin sets
	// and understands how to decode
	// this is used to allow modification and migration of the task schema
	// used by the plugin
	taskHandleVersion = 1
)

var (
	// pluginInfo describes the plugin
	pluginInfo = &base.PluginInfoResponse{
		Type:              base.PluginTypeDriver,
		PluginApiVersions: []string{drivers.ApiVersion010},
		PluginVersion:     pluginVersion,
		Name:              pluginName,
	}

	// capabilities indicates what optional features this driver supports
	// this should be set according to the target run time.
	capabilities = &drivers.Capabilities{
		// The plugin's capabilities signal Nomad which extra functionalities
		// are supported. For a list of available options check the docs page:
		// https://godoc.org/github.com/hashicorp/nomad/plugins/drivers#Capabilities
		SendSignals: true,
		Exec:        false,
		FSIsolation: drivers.FSIsolationImage,
		RemoteTasks: true,
	}

	// configSpec is the specification of the plugin's configuration
	// this is used to validate the configuration specified for the plugin
	// on the client.
	// this is not global, but can be specified on a per-client basis.
	configSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		// TODO: define plugin's agent configuration schema.
		//
		// The schema should be defined using HCL specs and it will be used to
		// validate the agent configuration provided by the user in the
		// `plugin` stanza (https://www.nomadproject.io/docs/configuration/plugin.html).
		//
		// For example, for the schema below a valid configuration would be:
		//
		//   plugin "hello-driver-plugin" {
		//     config {
		//       shell = "fish"
		//     }
		//   }
		//"shell": hclspec.NewDefault(
		//"shell"	hclspec.NewAttr("shell", "string", false),
		//"shell"	hclspec.NewLiteral(`"bash"`),
		//"shell"),
		"enabled":   hclspec.NewAttr("enabled", "bool", false),
		"cloudapi":  hclspec.NewAttr("cloudapi", "bool", false),
		"dockerapi": hclspec.NewAttr("dockerapi", "bool", false),
		"cluster":   hclspec.NewAttr("cluster", "string", false),
		"region":    hclspec.NewAttr("region", "string", false),
	})

	// taskConfigSpec is the specification of the plugin's configuration for
	// a task
	// this is used to validated the configuration specified for the plugin
	// when a job is submitted.
	taskConfigSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		"docker_api": hclspec.NewBlock("docker_api", false, hclspec.NewObject(map[string]*hclspec.Spec{
			"cmd":             hclspec.NewAttr("cmd", "list(string)", false),
			"entrypoint":      hclspec.NewAttr("entrypoint", "list(string)", false),
			"openstdin":       hclspec.NewAttr("openstdin", "bool", false),
			"stdinonce":       hclspec.NewAttr("stdinonce", "bool", false),
			"tty":             hclspec.NewAttr("tty", "bool", false),
			"workingdir":      hclspec.NewAttr("workingdir", "string", false),
			"hostname":        hclspec.NewAttr("hostname", "string", false),
			"dns":             hclspec.NewAttr("dns", "list(string)", false),
			"dns_search":      hclspec.NewAttr("dns_search", "list(string)", false),
			"extra_hosts":     hclspec.NewAttr("extra_hosts", "list(string)", false),
			"user":            hclspec.NewAttr("user", "string", false),
			"domain_name":     hclspec.NewAttr("domain_name", "string", false),
			"labels":          hclspec.NewBlockAttrs("labels", "string", false),
			"public_network":  hclspec.NewAttr("public_network", "string", false),
			"private_network": hclspec.NewAttr("private_network", "string", false),
			"log_config": hclspec.NewBlock("log_config", false, hclspec.NewObject(map[string]*hclspec.Spec{
				"type":   hclspec.NewAttr("type", "string", false),
				"config": hclspec.NewBlockAttrs("config", "string", false),
			})),
			"ports": hclspec.NewBlock("ports", false, hclspec.NewObject(map[string]*hclspec.Spec{
				"tcp":         hclspec.NewAttr("tcp", "list(number)", false),
				"udp":         hclspec.NewAttr("udp", "list(number)", false),
				"publish_all": hclspec.NewAttr("publish_all", "bool", false),
			})),
			"image": hclspec.NewBlock("image", true, hclspec.NewObject(map[string]*hclspec.Spec{
				"name":      hclspec.NewAttr("name", "string", true),
				"tag":       hclspec.NewAttr("tag", "string", false),
				"auto_pull": hclspec.NewAttr("auto_pull", "bool", false),
			})),
			"auth": hclspec.NewBlock("auth", false, hclspec.NewObject(map[string]*hclspec.Spec{
				"username":       hclspec.NewAttr("username", "string", false),
				"password":       hclspec.NewAttr("password", "string", false),
				"email":          hclspec.NewAttr("email", "string", false),
				"server_address": hclspec.NewAttr("server_address", "string", false),
			})),
			"restart_policy": hclspec.NewAttr("restart_policy", "string", false),
		})),
		"cloud_api": hclspec.NewBlock("cloud_api", false, hclspec.NewObject(map[string]*hclspec.Spec{
			"image": hclspec.NewBlock("image", true, hclspec.NewObject(map[string]*hclspec.Spec{
				"name":        hclspec.NewAttr("name", "string", false),
				"version":     hclspec.NewAttr("version", "string", false),
				"most_recent": hclspec.NewAttr("most_recent", "bool", false),
			})),
			"networks": hclspec.NewBlockList("networks", hclspec.NewObject(map[string]*hclspec.Spec{
				"name": hclspec.NewAttr("name", "string", false),
			})),
			"user_data":    hclspec.NewAttr("user_data", "string", false),
			"cloud_config": hclspec.NewAttr("cloud_config", "string", false),
			"user_script":  hclspec.NewAttr("user_script", "string", false),
		})),
		"tags":                hclspec.NewBlockAttrs("tags", "string", false),
		"affinity":            hclspec.NewAttr("affinity", "list(string)", false),
		"deletion_protection": hclspec.NewAttr("deletion_protection", "bool", false),
		"fwenabled":           hclspec.NewAttr("fwenabled", "bool", false),
		"fwrules":             hclspec.NewBlockAttrs("fwrules", "string", false),
		"cns":                 hclspec.NewAttr("cns", "list(string)", false),
		"package": hclspec.NewBlock("package", true, hclspec.NewObject(map[string]*hclspec.Spec{
			"name":    hclspec.NewAttr("name", "string", false),
			"version": hclspec.NewAttr("version", "string", false),
		})),
	})
)

// Config contains configuration information for the plugin
type DriverConfig struct {
	// decoded plugin configuration struct
	//
	// This struct is the decoded version of the schema defined in the
	// configSpec variable above. It's used to convert the HCL configuration
	// passed by the Nomad agent into Go contructs.
	//Shell string `codec:"shell"`
	Cluster          string `codec:"cluster"`
	Enabled          bool   `codec:"enabled"`
	CloudAPIEnabled  bool   `codec:"cloudapi"`
	DockerAPIEnabled bool   `codec:"dockerapi"`
	Region           string `codec:"region"`
}

type TaskConfig struct {
	Affinity           []string          `codec:"affinity" json:"affinity"`
	CNS                []string          `codec:"cns" json:"cns"`
	Cloud              CloudAPI          `codec:"cloud_api" json:"cloud_api"`
	DeletionProtection bool              `codec:"deletion_protection" json:"deletion_protection"`
	Docker             DockerAPI         `codec:"docker_api" json:"docker_api"`
	FWEnabled          bool              `codec:"fwenabled" json:"fwenabled"`
	Package            Package           `codec:"package" json:"package"`
	Tags               map[string]string `codec:"tags" json:"tags"`
}

type CloudAPI struct {
	CloudConfig string     `codec:"cloud_config" json:"cloud_config"`
	Image       CloudImage `codec:"image" json:"image"`
	Networks    []Network  `codec:"networks" json:"networks"`
	UserData    string     `codec:"user_data" json:"user_data"`
	UserScript  string     `codec:"user_script" json:"user_script"`
}

type Network struct {
	Name string `codec:"name" json:"name"`
}

type DockerAuth struct {
	Username   string `codec:"username" json:"username"`
	Password   string `codec:"password" json:"password"`
	Email      string `codec:"email" json:"email"`
	ServerAddr string `codec:"server_address" json:" server_address"`
}

type DockerImage struct {
	Name     string `codec:"name" json:"name"`
	Tag      string `codec:"tag" json:"tag"`
	AutoPull bool   `codec:"auto_pull" json:"auto_pull"`
}

type Package struct {
	Name    string `codec:"name" json:"name"`
	Version string `codec:"version" json:"version"`
}

type CloudImage struct {
	Name       string `codec:"name" json:"name"`
	MostRecent bool   `codec:"most_recent" json:"most_recent"`
	Version    string `codec:"version" json:"version"`
}

type Ports struct {
	TCP        []int `codec:"tcp" json:"tcp"`
	UDP        []int `codec:"udp" json:"udp"`
	PublishAll bool  `codec:"publish_all" json:"publish_all"`
}

type LogConfig struct {
	Type   string            `codec:"type" json:"type"`
	Config map[string]string `codec:"config" json:"config"`
}

type DockerAPI struct {
	Cmd            []string          `codec:"cmd" json:"cmd"`
	Entrypoint     []string          `codec:"entrypoint" json:"entrypoint"`
	OpenStdin      bool              `codec:"openstdin" json:"openstdin"`
	StdInOnce      bool              `codec:"stdinonce" json:"stdinonce"`
	TTY            bool              `codec:"tty" json:"tty"`
	WorkingDir     string            `codec:"workingdir" json:"workingdir"`
	Image          DockerImage       `codec:"image" json:"image"`
	Auth           DockerAuth        `codec:"auth" json:"auth"`
	Labels         map[string]string `codec:"labels" json:"labels"`
	PublicNetwork  string            `codec:"public_network" json:"public_network"`
	PrivateNetwork string            `codec:"private_network" json:"private_network"`
	RestartPolicy  string            `codec:"restart_policy" json:"restart_policy"`
	Ports          Ports             `codec:"ports" json:"ports"`
	Hostname       string            `codec:"hostname" json:"hostname"`
	DNS            []string          `codec:"dns" json:"dns"`
	DNSSearch      []string          `codec:"dns_search" json:"dns_search"`
	User           string            `codec:"user" json:"user"`
	Domainname     string            `codec:"domain_name" json:"domain_name"`
	ExtraHosts     []string          `codec:"extra_hosts" json:"extra_hosts"`
	LogConfig      LogConfig         `codec:"log_config" json:"log_config"`
}
