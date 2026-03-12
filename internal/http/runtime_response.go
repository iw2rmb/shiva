package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/gofiber/fiber/v2"
)

type runtimePreparedResponse struct {
	Status int
	Header http.Header
	Body   []byte
}

func buildRuntimeSuccessResponse(
	ctx context.Context,
	request runtimeValidatedRequest,
	resolved runtimeResolvedOperation,
	acceptHeader string,
) (runtimePreparedResponse, error) {
	status, responseRef, err := selectRuntimeSuccessResponse(resolved.Operation.Responses)
	if err != nil {
		return runtimePreparedResponse{}, err
	}

	response, err := buildRuntimePreparedResponse(status, responseRef, acceptHeader, true)
	if err != nil {
		return runtimePreparedResponse{}, err
	}

	if err := openapi3filter.ValidateResponse(ctx, &openapi3filter.ResponseValidationInput{
		RequestValidationInput: request.ValidationInput,
		Status:                 response.Status,
		Header:                 response.Header,
		Body:                   io.NopCloser(bytes.NewReader(response.Body)),
		Options: &openapi3filter.Options{
			IncludeResponseStatus: true,
		},
	}); err != nil {
		return runtimePreparedResponse{}, fmt.Errorf("validate runtime stub response: %w", err)
	}

	return response, nil
}

func writeRuntimeFailureResponse(
	c *fiber.Ctx,
	resolved runtimeResolvedOperation,
	failure *runtimeFailure,
) error {
	if failure == nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "runtime request failed",
		})
	}

	status := failure.preferredStatus()
	responses := resolved.Operation.Responses
	if responses != nil {
		if responseRef := responses.Status(status); responseRef != nil {
			response, err := buildRuntimePreparedResponse(status, responseRef, "", false)
			if err != nil {
				return err
			}
			return writeRuntimePreparedResponse(c, response)
		}
		if responseRef := responses.Default(); responseRef != nil {
			response, err := buildRuntimePreparedResponse(status, responseRef, "", false)
			if err != nil {
				return err
			}
			return writeRuntimePreparedResponse(c, response)
		}
	}

	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"error": failure.Error(),
	})
}

func writeRuntimePreparedResponse(c *fiber.Ctx, response runtimePreparedResponse) error {
	for key, values := range response.Header {
		c.Set(key, strings.Join(values, ", "))
	}
	if len(response.Body) == 0 {
		return c.Status(response.Status).Send(nil)
	}
	return c.Status(response.Status).Send(response.Body)
}

func selectRuntimeSuccessResponse(responses *openapi3.Responses) (int, *openapi3.ResponseRef, error) {
	if responses == nil {
		return 0, nil, fmt.Errorf("runtime operation responses are not defined")
	}

	statuses := make([]int, 0)
	for _, key := range sortedStringKeys(responses.Map()) {
		status, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		if status >= 200 && status < 300 {
			statuses = append(statuses, status)
		}
	}
	if len(statuses) == 0 {
		return 0, nil, fmt.Errorf("runtime operation does not declare an explicit success response")
	}
	sort.Ints(statuses)

	status := statuses[0]
	responseRef := responses.Status(status)
	if responseRef == nil {
		return 0, nil, fmt.Errorf("runtime success response %d is not resolved", status)
	}
	return status, responseRef, nil
}

func buildRuntimePreparedResponse(
	status int,
	responseRef *openapi3.ResponseRef,
	acceptHeader string,
	respectAccept bool,
) (runtimePreparedResponse, error) {
	if responseRef == nil || responseRef.Value == nil {
		return runtimePreparedResponse{}, fmt.Errorf("runtime response is not resolved")
	}

	response := responseRef.Value
	header := make(http.Header)
	for _, name := range sortedStringKeys(response.Headers) {
		headerRef := response.Headers[name]
		if headerRef == nil || headerRef.Value == nil || !headerRef.Value.Required {
			continue
		}

		value, err := buildRuntimeHeaderValue(headerRef.Value.Schema)
		if err != nil {
			return runtimePreparedResponse{}, fmt.Errorf("build runtime response header %q: %w", name, err)
		}
		header.Set(name, value)
	}

	body, contentType, err := buildRuntimeResponseBody(response.Content, acceptHeader, respectAccept)
	if err != nil {
		return runtimePreparedResponse{}, err
	}
	if contentType != "" {
		header.Set(fiber.HeaderContentType, contentType)
	}

	return runtimePreparedResponse{
		Status: status,
		Header: header,
		Body:   body,
	}, nil
}

func buildRuntimeResponseBody(
	content openapi3.Content,
	acceptHeader string,
	respectAccept bool,
) ([]byte, string, error) {
	if len(content) == 0 {
		return nil, "", nil
	}

	contentType, mediaType, err := selectRuntimeContentType(content, acceptHeader, respectAccept)
	if err != nil {
		return nil, "", err
	}

	value, err := runtimeResponseValue(mediaType)
	if err != nil {
		return nil, "", err
	}
	if value == nil {
		return nil, contentType, nil
	}

	body, err := marshalRuntimeBody(contentType, value)
	if err != nil {
		return nil, "", err
	}
	return body, contentType, nil
}

func selectRuntimeContentType(
	content openapi3.Content,
	acceptHeader string,
	respectAccept bool,
) (string, *openapi3.MediaType, error) {
	contentTypes := sortedStringKeys(content)
	if len(contentTypes) == 0 {
		return "", nil, fmt.Errorf("runtime response content is empty")
	}

	if !respectAccept || strings.TrimSpace(acceptHeader) == "" {
		return contentTypes[0], content[contentTypes[0]], nil
	}

	for _, candidate := range contentTypes {
		if acceptsRuntimeContentType(acceptHeader, candidate) {
			return candidate, content[candidate], nil
		}
	}

	return "", nil, &runtimeFailure{
		Class: runtimeFailureNotAcceptable,
		Err:   fmt.Errorf("accept header %q does not match runtime response content", acceptHeader),
	}
}

func acceptsRuntimeContentType(acceptHeader string, candidate string) bool {
	for _, rawPart := range strings.Split(acceptHeader, ",") {
		part := strings.TrimSpace(rawPart)
		if part == "" {
			continue
		}
		mediaType, params, err := mime.ParseMediaType(part)
		if err != nil {
			mediaType = part
			params = nil
		}
		if params != nil && strings.TrimSpace(params["q"]) == "0" {
			continue
		}
		if mediaType == "*/*" || strings.EqualFold(mediaType, candidate) {
			return true
		}
		if strings.HasSuffix(mediaType, "/*") {
			prefix := strings.TrimSuffix(strings.ToLower(mediaType), "*")
			if strings.HasPrefix(strings.ToLower(candidate), prefix) {
				return true
			}
		}
	}
	return false
}

func runtimeResponseValue(mediaType *openapi3.MediaType) (any, error) {
	if mediaType == nil {
		return nil, nil
	}
	if mediaType.Example != nil {
		return cloneRuntimeValue(mediaType.Example)
	}
	for _, key := range sortedStringKeys(mediaType.Examples) {
		exampleRef := mediaType.Examples[key]
		if exampleRef == nil || exampleRef.Value == nil {
			continue
		}
		return cloneRuntimeValue(exampleRef.Value.Value)
	}
	if mediaType.Schema == nil {
		return nil, nil
	}
	return generateRuntimeSchemaValue(mediaType.Schema)
}

func buildRuntimeHeaderValue(schemaRef *openapi3.SchemaRef) (string, error) {
	if schemaRef == nil {
		return "", nil
	}
	value, err := generateRuntimeSchemaValue(schemaRef)
	if err != nil {
		return "", err
	}
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, ","), nil
	default:
		return fmt.Sprint(value), nil
	}
}

func marshalRuntimeBody(contentType string, value any) ([]byte, error) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}
	if strings.HasSuffix(mediaType, "/json") || strings.HasSuffix(mediaType, "+json") {
		return json.Marshal(value)
	}
	if text, ok := value.(string); ok {
		return []byte(text), nil
	}
	return json.Marshal(value)
}

func cloneRuntimeValue(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return value, nil
	}
	var cloned any
	if err := json.Unmarshal(data, &cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

func generateRuntimeSchemaValue(schemaRef *openapi3.SchemaRef) (any, error) {
	if schemaRef == nil || schemaRef.Value == nil {
		return nil, nil
	}

	schema := schemaRef.Value
	if schema.Example != nil {
		return cloneRuntimeValue(schema.Example)
	}
	if schema.Default != nil {
		return cloneRuntimeValue(schema.Default)
	}
	if len(schema.Enum) > 0 {
		return cloneRuntimeValue(schema.Enum[0])
	}
	if len(schema.OneOf) > 0 {
		return generateRuntimeSchemaValue(schema.OneOf[0])
	}
	if len(schema.AnyOf) > 0 {
		return generateRuntimeSchemaValue(schema.AnyOf[0])
	}
	if len(schema.AllOf) > 0 {
		merged := make(map[string]any)
		for _, child := range schema.AllOf {
			value, err := generateRuntimeSchemaValue(child)
			if err != nil {
				return nil, err
			}
			objectValue, ok := value.(map[string]any)
			if !ok {
				return value, nil
			}
			for key, item := range objectValue {
				merged[key] = item
			}
		}
		return merged, nil
	}

	switch runtimeSchemaType(schema) {
	case "object":
		properties := make(map[string]any)
		required := make(map[string]struct{}, len(schema.Required))
		for _, name := range schema.Required {
			required[name] = struct{}{}
		}

		names := sortedStringKeys(schema.Properties)
		for _, name := range names {
			propertyRef := schema.Properties[name]
			if propertyRef == nil {
				continue
			}
			if _, ok := required[name]; !ok && uint64(len(properties)) >= schema.MinProps {
				continue
			}
			value, err := generateRuntimeSchemaValue(propertyRef)
			if err != nil {
				return nil, err
			}
			properties[name] = value
		}
		for uint64(len(properties)) < schema.MinProps && schema.AdditionalProperties.Schema != nil {
			key := fmt.Sprintf("property_%d", len(properties)+1)
			value, err := generateRuntimeSchemaValue(schema.AdditionalProperties.Schema)
			if err != nil {
				return nil, err
			}
			properties[key] = value
		}
		return properties, nil
	case "array":
		count := int(schema.MinItems)
		items := make([]any, 0, count)
		for i := 0; i < count; i++ {
			value, err := generateRuntimeSchemaValue(schema.Items)
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
		return items, nil
	case "integer":
		if schema.Min != nil {
			return int64(*schema.Min), nil
		}
		return int64(0), nil
	case "number":
		if schema.Min != nil {
			return *schema.Min, nil
		}
		return 0.0, nil
	case "boolean":
		return false, nil
	case "string":
		return runtimeStringValue(schema), nil
	case "null":
		return nil, nil
	default:
		if schema.Nullable {
			return nil, nil
		}
		return map[string]any{}, nil
	}
}

func runtimeSchemaType(schema *openapi3.Schema) string {
	if schema == nil || schema.Type == nil || len(*schema.Type) == 0 {
		return ""
	}
	for _, candidate := range *schema.Type {
		if candidate != "null" {
			return candidate
		}
	}
	return "null"
}

func runtimeStringValue(schema *openapi3.Schema) string {
	if schema == nil {
		return ""
	}
	switch strings.ToLower(schema.Format) {
	case "date":
		return "2000-01-01"
	case "date-time":
		return "2000-01-01T00:00:00Z"
	case "uuid":
		return "00000000-0000-0000-0000-000000000000"
	case "byte":
		return "AA=="
	}

	length := int(schema.MinLength)
	if length < 1 {
		length = 1
	}
	return strings.Repeat("a", length)
}
