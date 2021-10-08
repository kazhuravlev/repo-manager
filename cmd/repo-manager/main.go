package main

import (
	"fmt"
	"log"
	"os"

	repomgr "github.com/kazhuravlev/repo-manager/internal/repo-manager"
	"github.com/urfave/cli/v2"
)

const (
	keySpecFilename = "spec"
)

func main() {
	app := &cli.App{
		Name: "repo-manager",
		Commands: []*cli.Command{
			{
				Name:        "run",
				Description: "Run checker",
				Action:      cmdRun,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  keySpecFilename,
						Value: "repo-manager-rules.yml",
					},
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func cmdRun(c *cli.Context) error {
	spec, err := repomgr.ParseSpec(c.String(keySpecFilename))
	if err != nil {
		return fmt.Errorf("cannot parse spec: %w", err)
	}

	manager, err := repomgr.New(repomgr.NewOptions(*spec))
	if err != nil {
		return fmt.Errorf("cannot create manager instance: %w", err)
	}

	if err := manager.Run(); err != nil {
		return fmt.Errorf("error on run manager: %w", err)
	}

	return nil
}
