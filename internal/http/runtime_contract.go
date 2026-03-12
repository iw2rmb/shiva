package httpserver

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
)

const (
	runtimeRoutePrefix       = "/gl"
	runtimeSelectorLatest    = "latest"
	runtimeSelectorSHALength = 8
)

var runtimeSupportedMethods = []string{
	"GET",
	"PUT",
	"POST",
	"DELETE",
	"OPTIONS",
	"HEAD",
	"PATCH",
	"TRACE",
}

// These declarations pin the runtime validation contract to kin-openapi.
// Shiva resolves /gl/* routes dynamically from stored specs, so static
// per-spec middleware such as generated Fiber adapters is not a fit.
var (
	_ *openapi3.T
	_ openapi3filter.AuthenticationFunc
)
