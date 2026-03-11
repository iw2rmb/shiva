package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/iw2rmb/shiva/internal/cli"
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
			cfg, err := cli.LoadConfigFromEnv()
			if err != nil {
				serviceError = err
				return
			}

			httpClient, err := cli.NewHTTPClient(cfg)
			if err != nil {
				serviceError = err
				return
			}

			service = cli.NewService(httpClient)
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
