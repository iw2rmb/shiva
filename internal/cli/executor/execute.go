package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

func Execute(ctx context.Context, plan CallPlan) (HTTPResponse, error) {
	if !plan.Dispatch.Network {
		return HTTPResponse{}, fmt.Errorf("dispatch plan is not executable")
	}
	if plan.Dispatch.Timeout <= 0 {
		return HTTPResponse{}, fmt.Errorf("dispatch timeout must be greater than zero")
	}

	requestBody := bytes.NewReader(plan.Dispatch.Request.Body)
	req, err := http.NewRequestWithContext(ctx, plan.Dispatch.Request.Method, plan.Dispatch.Request.URL, requestBody)
	if err != nil {
		return HTTPResponse{}, fmt.Errorf("build dispatch request: %w", err)
	}
	for key, values := range plan.Dispatch.Request.Headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	resp, err := (&http.Client{Timeout: plan.Dispatch.Timeout}).Do(req)
	if err != nil {
		return HTTPResponse{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return HTTPResponse{}, fmt.Errorf("read dispatch response body: %w", err)
	}

	return HTTPResponse{
		StatusCode: resp.StatusCode,
		Headers:    cloneHeaders(resp.Header),
		Body:       body,
	}, nil
}
