package cli

import "github.com/iw2rmb/shiva/internal/cli/httpclient"

type SpecFormat = httpclient.SpecFormat

const (
	SpecFormatJSON = httpclient.SpecFormatJSON
	SpecFormatYAML = httpclient.SpecFormatYAML
)

type CallFormat string

const (
	CallFormatBody CallFormat = "body"
	CallFormatJSON CallFormat = "json"
	CallFormatCurl CallFormat = "curl"
)

type BatchFormat string

const (
	BatchFormatJSON   BatchFormat = "json"
	BatchFormatNDJSON BatchFormat = "ndjson"
)
