package triton

import (
	"github.com/hashicorp/nomad/plugins/shared/hclspec"
)

var (
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
	})

	// taskConfigSpec is the specification of the plugin's configuration for
	// a task
	// this is used to validated the configuration specified for the plugin
	// when a job is submitted.
	taskConfigSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		"api_type": hclspec.NewAttr("api_type", "string", true),
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
				"uuid":        hclspec.NewAttr("uuid", "string", false),
				"version":     hclspec.NewAttr("version", "string", false),
				"most_recent": hclspec.NewAttr("most_recent", "bool", false),
			})),
			"networks": hclspec.NewBlockList("networks", hclspec.NewObject(map[string]*hclspec.Spec{
				"name": hclspec.NewAttr("name", "string", false),
				"uuid": hclspec.NewAttr("uuid", "string", false),
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
			"uuid":    hclspec.NewAttr("uuid", "string", false),
			"version": hclspec.NewAttr("version", "string", false),
		})),
		"exit_strategy": hclspec.NewAttr("exit_strategy", "string", false),
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
}

type TaskConfig struct {
	APIType            string            `codec:"api_type" json:"api_type"`
	Cloud              CloudAPI          `codec:"cloud_api" json:"cloud_api"`
	Docker             DockerAPI         `codec:"docker_api" json:"docker_api"`
	Affinity           []string          `codec:"affinity" json:"affinity"`
	CNS                []string          `codec:"cns" json:"cns"`
	DeletionProtection bool              `codec:"deletion_protection" json:"deletion_protection"`
	FWEnabled          bool              `codec:"fwenabled" json:"fwenabled"`
	FWRules            map[string]string `codec:"fwrules" json:"fwrules"`
	Package            Package           `codec:"package" json:"package"`
	ExitStrategy       string            `codec:"exit_strategy" json:"exit_strategy"`
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
	UUID string `codec:"uuid" json:"uuid"`
}

type DockerAuth struct {
	Username   string `codec:"username"`
	Password   string `codec:"password"`
	Email      string `codec:"email"`
	ServerAddr string `codec:"server_address"`
}

type DockerImage struct {
	Name     string `codec:"name" json:"name"`
	Tag      string `codec:"tag" json:"tag"`
	AutoPull bool   `codec:"auto_pull" json:"auto_pull"`
}

type Package struct {
	Name    string `codec:"name" json:"name"`
	UUID    string `codec:"uuid" json:"uuid"`
	Version string `codec:"version" json:"version"`
}

type CloudImage struct {
	Name       string `codec:"name" json:"name"`
	UUID       string `codec:"uuid" json:"uuid"`
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
	Auth           DockerAuth        `codec:"auth"`
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
