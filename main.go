package main

import (
	"github.com/Lucretius/nomad-elastigroup-autoscaler/plugin"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-autoscaler/plugins"
)

func main() {
	plugins.Serve(factory)
}

// factory returns a new instance of the Spotinst Elastigroup plugin.
func factory(log hclog.Logger) interface{} {
	return plugin.NewSpotinstElastigroupPlugin(log)
}
