package markdown

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type EndpointInput struct {
	Method    string
	Path      string
	Operation json.RawMessage
}

type operationDocument struct {
	OperationID string                    `json:"operationId"`
	Summary     string                    `json:"summary"`
	Description string                    `json:"description"`
	Deprecated  bool                      `json:"deprecated"`
	Parameters  []parameterDocument       `json:"parameters"`
	RequestBody *requestBodyDocument      `json:"requestBody"`
	Responses   map[string]responseObject `json:"responses"`
	Servers     []serverDocument          `json:"servers"`
}

type parameterDocument struct {
	Name        string      `json:"name"`
	In          string      `json:"in"`
	Required    bool        `json:"required"`
	Description string      `json:"description"`
	Deprecated  bool        `json:"deprecated"`
	Schema      *schemaNode `json:"schema"`
	Example     any         `json:"example"`
}

type requestBodyDocument struct {
	Description string                 `json:"description"`
	Required    bool                   `json:"required"`
	Content     map[string]mediaObject `json:"content"`
}

type responseObject struct {
	Description string                 `json:"description"`
	Content     map[string]mediaObject `json:"content"`
}

type mediaObject struct {
	Schema *schemaNode `json:"schema"`
}

type serverDocument struct {
	URL         string                    `json:"url"`
	Description string                    `json:"description"`
	Variables   map[string]serverVariable `json:"variables"`
}

type serverVariable struct {
	Default     string   `json:"default"`
	Enum        []string `json:"enum"`
	Description string   `json:"description"`
}

type specDocument struct {
	Servers []serverDocument `json:"servers"`
}

type schemaNode struct {
	Ref         string                `json:"$ref"`
	Title       string                `json:"title"`
	Type        string                `json:"type"`
	Format      string                `json:"format"`
	Description string                `json:"description"`
	Nullable    bool                  `json:"nullable"`
	Enum        []any                 `json:"enum"`
	Required    []string              `json:"required"`
	Items       *schemaNode           `json:"items"`
	Properties  map[string]schemaNode `json:"properties"`
	OneOf       []schemaNode          `json:"oneOf"`
	AnyOf       []schemaNode          `json:"anyOf"`
	AllOf       []schemaNode          `json:"allOf"`
}

func BuildEndpoint(input EndpointInput) string {
	method := strings.ToUpper(strings.TrimSpace(input.Method))
	if method == "" {
		method = "UNKNOWN"
	}
	path := strings.TrimSpace(input.Path)
	if path == "" {
		path = "/"
	}

	sections := []string{fmt.Sprintf("## %s %s", method, path)}

	operation, ok := decodeOperation(input.Operation)
	if !ok {
		sections = append(sections, "_Failed to decode operation payload._")
		return strings.Join(sections, "\n\n")
	}

	if operation.OperationID != "" {
		sections = append(sections, fmt.Sprintf("`Operation ID:` `%s`", operation.OperationID))
	}
	if operation.Summary != "" {
		sections = append(sections, fmt.Sprintf("`Summary:` %s", operation.Summary))
	}
	if operation.Description != "" {
		sections = append(sections, operation.Description)
	}
	if operation.Deprecated {
		sections = append(sections, "`Deprecated:` `true`")
	}

	sections = append(sections, renderParametersSection(operation.Parameters))
	sections = append(sections, renderRequestBodySection(operation.RequestBody))
	sections = append(
		sections,
		renderResponsesSection(
			operation.Responses,
			func(string) bool { return true },
			"### Responses",
			"No documented responses.",
		),
	)

	return strings.Join(filterNonEmpty(sections), "\n\n")
}

func BuildServers(operationBody json.RawMessage, specBody json.RawMessage) string {
	operation, _ := decodeOperation(operationBody)
	if len(operation.Servers) > 0 {
		return renderServersSection("operation", operation.Servers)
	}

	spec, ok := decodeSpec(specBody)
	if ok && len(spec.Servers) > 0 {
		return renderServersSection("spec", spec.Servers)
	}
	return BuildEmptyServers()
}

func BuildErrors(operationBody json.RawMessage) string {
	operation, ok := decodeOperation(operationBody)
	if !ok {
		return BuildEmptyErrors()
	}
	return renderResponsesSection(
		operation.Responses,
		func(code string) bool {
			return code == "default" || !is2xxStatus(code)
		},
		"## Errors",
		"No documented non-2xx or default responses.",
	)
}

func BuildEmptyServers() string {
	return strings.Join([]string{
		"## Servers",
		"",
		"No servers documented for this endpoint or API spec.",
	}, "\n")
}

func BuildEmptyErrors() string {
	return strings.Join([]string{
		"## Errors",
		"",
		"No documented non-2xx or default responses.",
	}, "\n")
}

func decodeOperation(raw json.RawMessage) (operationDocument, bool) {
	var operation operationDocument
	if len(raw) == 0 {
		return operation, false
	}
	if err := json.Unmarshal(raw, &operation); err != nil {
		return operation, false
	}
	return operation, true
}

func decodeSpec(raw json.RawMessage) (specDocument, bool) {
	var spec specDocument
	if len(raw) == 0 {
		return spec, false
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		return spec, false
	}
	return spec, true
}

func renderServersSection(source string, servers []serverDocument) string {
	parts := []string{
		"## Servers",
		fmt.Sprintf("`Source:` `%s`", source),
	}

	for _, server := range servers {
		header := fmt.Sprintf("### `%s`", strings.TrimSpace(server.URL))
		if server.URL == "" {
			header = "### `unknown`"
		}
		parts = append(parts, header)
		if server.Description != "" {
			parts = append(parts, server.Description)
		}
		if len(server.Variables) > 0 {
			encoded, err := json.MarshalIndent(server.Variables, "", "  ")
			if err != nil {
				continue
			}
			parts = append(parts, "```json\n"+string(encoded)+"\n```")
		}
	}

	return strings.Join(filterNonEmpty(parts), "\n\n")
}

func renderParametersSection(parameters []parameterDocument) string {
	if len(parameters) == 0 {
		return strings.Join([]string{
			"### Parameters",
			"",
			"No documented parameters.",
		}, "\n")
	}

	grouped := map[string][]parameterDocument{}
	for _, parameter := range parameters {
		location := strings.ToLower(strings.TrimSpace(parameter.In))
		if location == "" {
			location = "unknown"
		}
		grouped[location] = append(grouped[location], parameter)
	}

	order := []string{"path", "query", "header", "cookie"}
	remaining := make([]string, 0, len(grouped))
	for location := range grouped {
		if contains(order, location) {
			continue
		}
		remaining = append(remaining, location)
	}
	sort.Strings(remaining)
	order = append(order, remaining...)

	parts := []string{"### Parameters"}
	for _, location := range order {
		group := grouped[location]
		if len(group) == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("#### %s Parameters", strings.Title(location)))
		parts = append(parts, renderParameterBlock(location, group))
	}
	return strings.Join(parts, "\n\n")
}

func renderRequestBodySection(requestBody *requestBodyDocument) string {
	if requestBody == nil {
		return strings.Join([]string{
			"### Request Body",
			"",
			"No documented request body.",
		}, "\n")
	}

	parts := []string{"### Request Body"}
	if requestBody.Description != "" {
		parts = append(parts, requestBody.Description)
	}
	if requestBody.Required {
		parts = append(parts, "`Required:` `true`")
	}

	contentTypes := sortedKeys(requestBody.Content)
	if len(contentTypes) == 0 {
		parts = append(parts, "No documented request body schema.")
		return strings.Join(parts, "\n\n")
	}

	for _, contentType := range contentTypes {
		parts = append(parts, fmt.Sprintf("#### `%s`", contentType))
		schema := requestBody.Content[contentType].Schema
		if schema == nil {
			parts = append(parts, "_No schema defined._")
			continue
		}
		parts = append(parts, renderSchemaBlock("body", schema))
	}
	return strings.Join(parts, "\n\n")
}

func renderResponsesSection(
	responses map[string]responseObject,
	allow func(code string) bool,
	header string,
	emptyMessage string,
) string {
	codes := sortedResponseCodes(responses)
	filtered := make([]string, 0, len(codes))
	for _, code := range codes {
		if allow(code) {
			filtered = append(filtered, code)
		}
	}
	if len(filtered) == 0 {
		return strings.Join([]string{
			header,
			"",
			emptyMessage,
		}, "\n")
	}

	parts := []string{header}
	for _, code := range filtered {
		response := responses[code]
		title := fmt.Sprintf("#### `%s`", code)
		if response.Description != "" {
			title = fmt.Sprintf("#### `%s` %s", code, response.Description)
		}
		parts = append(parts, title)

		contentTypes := sortedKeys(response.Content)
		if len(contentTypes) == 0 {
			parts = append(parts, "_No response body schema._")
			continue
		}
		for _, contentType := range contentTypes {
			parts = append(parts, fmt.Sprintf("`%s`", contentType))
			schema := response.Content[contentType].Schema
			if schema == nil {
				parts = append(parts, "_No schema defined._")
				continue
			}
			parts = append(parts, renderSchemaBlock("body", schema))
		}
	}

	return strings.Join(parts, "\n\n")
}

func renderParameterBlock(location string, parameters []parameterDocument) string {
	sort.Slice(parameters, func(i int, j int) bool {
		if parameters[i].Required != parameters[j].Required {
			return parameters[i].Required
		}
		return strings.ToLower(parameters[i].Name) < strings.ToLower(parameters[j].Name)
	})

	lines := []string{"```ts"}
	for _, parameter := range parameters {
		lines = append(lines, parameterToLine(location, parameter))
	}
	lines = append(lines, "```")
	return strings.Join(lines, "\n")
}

func renderSchemaBlock(scope string, schema *schemaNode) string {
	lines := []string{"```ts"}
	title := schemaTitle(schema)
	if scope == "body" {
		lines = append(lines, fmt.Sprintf("%s {", title))
	} else {
		lines = append(lines, "{")
	}

	required := make(map[string]struct{}, len(schema.Required))
	for _, field := range schema.Required {
		required[field] = struct{}{}
	}

	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	sort.Slice(names, func(i int, j int) bool {
		_, iRequired := required[names[i]]
		_, jRequired := required[names[j]]
		if iRequired != jRequired {
			return iRequired
		}
		return names[i] < names[j]
	})

	if len(names) == 0 {
		lines = append(lines, "\tvalue: "+schemaType(schema))
		lines = append(lines, "}")
		lines = append(lines, "```")
		return strings.Join(lines, "\n")
	}

	for _, name := range names {
		property := schema.Properties[name]
		if property.Description != "" && strings.Contains(property.Description, "\n") {
			for _, line := range splitNonEmptyLines(property.Description) {
				lines = append(lines, "\t// "+line)
			}
		}
		marker := ""
		if _, ok := required[name]; ok {
			marker = "* "
		}
		inlineDescription := ""
		if property.Description != "" && !strings.Contains(property.Description, "\n") {
			inlineDescription = " // " + property.Description
		}
		lines = append(lines, fmt.Sprintf("\t%s%s: %s%s", marker, name, schemaType(&property), inlineDescription))
	}

	lines = append(lines, "}")
	lines = append(lines, "```")
	return strings.Join(lines, "\n")
}

func parameterToLine(location string, parameter parameterDocument) string {
	name := strings.TrimSpace(parameter.Name)
	switch location {
	case "path":
		name = "/{" + name + "}/"
	case "query":
		name = "&" + name
	case "header":
		name = "-H '" + name + "'"
	case "cookie":
		name = "Cookie " + name
	}

	required := ""
	if parameter.Required {
		required = "* "
	}

	parameterType := "unspecified"
	if parameter.Schema != nil {
		parameterType = schemaType(parameter.Schema)
	}

	extra := ""
	if parameter.Example != nil {
		extra = " = " + literalValue(parameter.Example)
	} else if parameter.Schema != nil && len(parameter.Schema.Enum) > 0 {
		values := make([]string, 0, len(parameter.Schema.Enum))
		for _, value := range parameter.Schema.Enum {
			values = append(values, literalValue(value))
		}
		extra = " = " + strings.Join(values, " | ")
	}
	if parameter.Schema != nil && parameter.Schema.Nullable {
		extra += " | null"
	}

	description := strings.TrimSpace(parameter.Description)
	if description != "" {
		description = " // " + firstLine(description)
	}
	return "\t" + required + name + ": " + parameterType + extra + description
}

func schemaType(schema *schemaNode) string {
	if schema == nil {
		return "unspecified"
	}
	if schema.Ref != "" {
		return refName(schema.Ref)
	}

	if len(schema.Enum) > 0 {
		values := make([]string, 0, len(schema.Enum))
		for _, value := range schema.Enum {
			values = append(values, literalValue(value))
		}
		typeExpr := strings.Join(values, " | ")
		if schema.Nullable {
			return typeExpr + " | null"
		}
		return typeExpr
	}

	if len(schema.OneOf) > 0 {
		return unionType(schema.OneOf)
	}
	if len(schema.AnyOf) > 0 {
		return unionType(schema.AnyOf)
	}
	if len(schema.AllOf) > 0 {
		return intersectionType(schema.AllOf)
	}

	base := strings.TrimSpace(schema.Type)
	if base == "" {
		if len(schema.Properties) > 0 {
			base = "object"
		} else if schema.Items != nil {
			base = "array"
		} else {
			base = "unspecified"
		}
	}

	if base == "array" && schema.Items != nil {
		base = "Array<" + schemaType(schema.Items) + ">"
	} else if schema.Format != "" {
		base = base + "(" + schema.Format + ")"
	}
	if schema.Nullable {
		base += " | null"
	}
	return base
}

func schemaTitle(schema *schemaNode) string {
	if schema == nil {
		return "Body"
	}
	title := strings.TrimSpace(schema.Title)
	if title == "" {
		return "Body"
	}
	return title
}

func unionType(parts []schemaNode) string {
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		value := schemaType(&part)
		if value != "" {
			result = append(result, value)
		}
	}
	if len(result) == 0 {
		return "unspecified"
	}
	return strings.Join(result, " | ")
}

func intersectionType(parts []schemaNode) string {
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		value := schemaType(&part)
		if value != "" {
			result = append(result, value)
		}
	}
	if len(result) == 0 {
		return "unspecified"
	}
	return strings.Join(result, " & ")
}

func refName(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "unspecified"
	}
	if idx := strings.LastIndex(ref, "/"); idx >= 0 && idx+1 < len(ref) {
		return ref[idx+1:]
	}
	return ref
}

func sortedResponseCodes(responses map[string]responseObject) []string {
	if len(responses) == 0 {
		return nil
	}
	type entry struct {
		code      string
		numeric   bool
		value     int
		isDefault bool
	}
	entries := make([]entry, 0, len(responses))
	for code := range responses {
		codeTrimmed := strings.TrimSpace(code)
		parsed, err := strconv.Atoi(codeTrimmed)
		entries = append(entries, entry{
			code:      codeTrimmed,
			numeric:   err == nil,
			value:     parsed,
			isDefault: codeTrimmed == "default",
		})
	}

	sort.Slice(entries, func(i int, j int) bool {
		if entries[i].isDefault != entries[j].isDefault {
			return !entries[i].isDefault
		}
		if entries[i].numeric != entries[j].numeric {
			return entries[i].numeric
		}
		if entries[i].numeric && entries[i].value != entries[j].value {
			return entries[i].value < entries[j].value
		}
		return entries[i].code < entries[j].code
	})

	codes := make([]string, 0, len(entries))
	for _, entry := range entries {
		codes = append(codes, entry.code)
	}
	return codes
}

func is2xxStatus(code string) bool {
	numeric, err := strconv.Atoi(strings.TrimSpace(code))
	if err != nil {
		return false
	}
	return numeric >= 200 && numeric < 300
}

func literalValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strconv.Quote(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case bool:
		return strconv.FormatBool(typed)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return "unspecified"
		}
		return string(encoded)
	}
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func splitNonEmptyLines(value string) []string {
	lines := strings.Split(value, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		result = append(result, line)
	}
	return result
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func filterNonEmpty(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
