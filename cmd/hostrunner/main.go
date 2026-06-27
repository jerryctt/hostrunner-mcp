package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jerryctt/hostrunner-mcp/internal/config"
	"github.com/jerryctt/hostrunner-mcp/internal/exec"
	internalserver "github.com/jerryctt/hostrunner-mcp/internal/server"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	cfgFlag := flag.String("config", "", "path to config.yaml (default: ~/.config/hostrunner/config.yaml, or set HOSTRUNNER_CONFIG)")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	log := zerolog.New(os.Stderr).With().Timestamp().Logger()

	cfgPath := config.ResolvePath(*cfgFlag)
	if cfgPath == "" {
		log.Fatal().Msg("cannot determine config path; pass -config or set HOSTRUNNER_CONFIG")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatal().Err(err).Str("path", cfgPath).Msg("load config (create it from examples/config.example.yaml, or set HOSTRUNNER_CONFIG / -config)")
	}

	s := internalserver.New(cfg, exec.Runner{}, log)
	if err := mcpserver.ServeStdio(s); err != nil {
		log.Fatal().Err(err).Msg("server exited")
	}
}
