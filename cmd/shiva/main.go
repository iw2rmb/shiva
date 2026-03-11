package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/iw2rmb/shiva/internal/cli"
	"github.com/iw2rmb/shiva/internal/cli/catalog"
	"github.com/iw2rmb/shiva/internal/cli/config"
)

func main() {
	os.Exit(run(context.Background(), os.Stdout, os.Stderr))
}

func run(ctx context.Context, stdout *os.File, stderr *os.File) int {
	var (
		once         sync.Once
		service      cli.Service
		serviceError error
	)

	command := cli.NewRootCommand(func() (cli.Service, error) {
		once.Do(func() {
			paths, err := config.ResolvePaths()
			if err != nil {
				serviceError = &cli.InvalidInputError{Message: err.Error()}
				return
			}

			document, err := config.LoadDocument(config.LoadOptions{
				ConfigHome: paths.ConfigHome,
			})
			if err != nil {
				serviceError = &cli.InvalidInputError{Message: err.Error()}
				return
			}

			catalogStore, err := catalog.NewStore(paths.CacheHome)
			if err != nil {
				serviceError = &cli.InvalidInputError{Message: err.Error()}
				return
			}

			service = cli.NewService(document, catalogStore)
		})

		return service, serviceError
	})
	command.SetOut(stdout)
	command.SetErr(stderr)

	if err := command.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return cli.ExitCode(err)
	}
	return cli.ExitCode(nil)
}
