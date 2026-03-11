package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/iw2rmb/shiva/internal/cli/executor"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

type payloadView struct {
	JSON json.RawMessage `json:"json,omitempty"`
	Text string          `json:"text,omitempty"`
}

type httpRequestView struct {
	Method  string              `json:"method,omitempty"`
	URL     string              `json:"url,omitempty"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    *payloadView        `json:"body,omitempty"`
}

type dispatchPlanView struct {
	Mode    executor.DispatchMode `json:"mode"`
	DryRun  bool                  `json:"dry_run"`
	Network bool                  `json:"network"`
	Request httpRequestView       `json:"request"`
}

type callPlanView struct {
	Request  request.Envelope `json:"request"`
	Dispatch dispatchPlanView `json:"dispatch"`
}

type httpResponseView struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Body       *payloadView        `json:"body,omitempty"`
}

type callResultView struct {
	Request  request.Envelope `json:"request"`
	Dispatch dispatchPlanView `json:"dispatch"`
	Response httpResponseView `json:"response"`
}

func RenderCallPlanJSON(plan executor.CallPlan) ([]byte, error) {
	return json.Marshal(callPlanToView(plan))
}

func RenderCallResultJSON(plan executor.CallPlan, response executor.HTTPResponse) ([]byte, error) {
	return json.Marshal(callResultView{
		Request:  plan.Request,
		Dispatch: dispatchToView(plan.Dispatch),
		Response: responseToView(response),
	})
}

func RenderCallBody(response executor.HTTPResponse) []byte {
	if len(response.Body) == 0 {
		return nil
	}
	return append([]byte(nil), response.Body...)
}

func RenderCallCurl(plan executor.CallPlan) ([]byte, error) {
	if plan.Dispatch.Mode != executor.DispatchModeDirect {
		return nil, fmt.Errorf("curl output is supported only for direct-call dry runs")
	}

	parts := []string{
		"curl",
		"-X",
		plan.Dispatch.Request.Method,
		shellQuote(plan.Dispatch.Request.URL),
	}

	for _, header := range flattenHeaders(plan.Dispatch.Request.Headers) {
		parts = append(parts, "-H", shellQuote(header))
	}
	if len(plan.Dispatch.Request.Body) > 0 {
		parts = append(parts, "--data-binary", shellQuote(string(plan.Dispatch.Request.Body)))
	}

	return []byte(strings.Join(parts, " ")), nil
}

func callPlanToView(plan executor.CallPlan) callPlanView {
	return callPlanView{
		Request:  plan.Request,
		Dispatch: dispatchToView(plan.Dispatch),
	}
}

func dispatchToView(plan executor.DispatchPlan) dispatchPlanView {
	return dispatchPlanView{
		Mode:    plan.Mode,
		DryRun:  plan.DryRun,
		Network: plan.Network,
		Request: httpRequestView{
			Method:  plan.Request.Method,
			URL:     plan.Request.URL,
			Headers: cloneListMap(plan.Request.Headers),
			Body:    payloadFromBytes(plan.Request.Body),
		},
	}
}

func responseToView(response executor.HTTPResponse) httpResponseView {
	return httpResponseView{
		StatusCode: response.StatusCode,
		Headers:    cloneListMap(response.Headers),
		Body:       payloadFromBytes(response.Body),
	}
}

func payloadFromBytes(body []byte) *payloadView {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil
	}
	if json.Valid(trimmed) {
		return &payloadView{
			JSON: append(json.RawMessage(nil), trimmed...),
		}
	}
	return &payloadView{Text: string(body)}
}

func flattenHeaders(headers map[string][]string) []string {
	if len(headers) == 0 {
		return nil
	}

	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	values := make([]string, 0, len(headers))
	for _, key := range keys {
		for _, value := range headers[key] {
			values = append(values, key+": "+value)
		}
	}
	return values
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func cloneListMap(input map[string][]string) map[string][]string {
	if len(input) == 0 {
		return nil
	}

	output := make(map[string][]string, len(input))
	for key, values := range input {
		copied := make([]string, len(values))
		copy(copied, values)
		output[key] = copied
	}
	return output
}
