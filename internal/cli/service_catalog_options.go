package cli

import "github.com/iw2rmb/shiva/internal/cli/catalog"

func catalogOptions(options RequestOptions) catalog.RefreshOptions {
	return catalog.RefreshOptions{
		Offline: options.Offline,
	}
}
