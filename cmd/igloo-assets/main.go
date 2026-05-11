package main

import (
	"os"

	"github.com/evanw/esbuild/pkg/api"
)

func main() {
	result := api.Build(api.BuildOptions{
		EntryPointsAdvanced: []api.EntryPoint{
			{InputPath: "static/js/src/feed/index.js", OutputPath: "feed"},
			{InputPath: "static/js/src/shorts/index.js", OutputPath: "shorts"},
			{InputPath: "static/js/src/player/index.js", OutputPath: "player"},
		},
		Bundle:    true,
		Format:    api.FormatIIFE,
		LogLevel:  api.LogLevelInfo,
		Outdir:    "static/js/dist",
		Platform:  api.PlatformBrowser,
		Sourcemap: api.SourceMapLinked,
		Write:     true,
	})
	if len(result.Errors) > 0 {
		os.Exit(1)
	}
}
