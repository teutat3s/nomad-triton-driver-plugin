package main

import (
	hclog "github.com/hashicorp/go-hclog"

	"github.com/hashicorp/nomad/plugins"

	"github.com/teutat3s/nomad-triton-driver-plugin/triton"
)

func main() {
	// Serve the plugin
	plugins.Serve(factory)
}

// factory returns a new instance of a nomad driver plugin
func factory(log hclog.Logger) interface{} {
	return triton.NewTritonDriver(log)
}
