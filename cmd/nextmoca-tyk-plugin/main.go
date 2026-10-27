package main

import (
	"log"

	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	"github.com/nextmoca/tyk-ai-studio-runtime-defense/internal/plugin"
)

func main() {
	log.Printf("starting %s plugin v%s", plugin.PluginName, plugin.PluginVersion)
	plugin_sdk.Serve(plugin.NewGatewayPlugin())
}
