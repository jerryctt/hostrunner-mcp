package main

import (
	"flag"
	"os"

	"github.com/jerryctt/hostrunner-mcp/internal/config"
	"github.com/jerryctt/hostrunner-mcp/internal/exec"
	internalserver "github.com/jerryctt/hostrunner-mcp/internal/server"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

func main() {
	cfgPath := flag.String("config", os.Getenv("HOSTRUNNER_CONFIG"), "path to config.yaml")
	flag.Parse()

	log := zerolog.New(os.Stderr).With().Timestamp().Logger()

	if *cfgPath == "" {
		log.Fatal().Msg("missing -config (or HOSTRUNNER_CONFIG)")
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal().Err(err).Msg("load config")
	}

	s := internalserver.New(cfg, exec.Runner{}, log)
	if err := mcpserver.ServeStdio(s); err != nil {
		log.Fatal().Err(err).Msg("server exited")
	}
}
