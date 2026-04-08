package tui

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	methodChipBaseStyle    = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	pathBaseStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	pathParamStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("#c11c84"))
	responseSuccessStyle   = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#6d8f56"))
	responseErrorChipStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#c96169"))
)

func methodChip(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "UNKNOWN"
	}
	style := methodChipBaseStyle.Foreground(lipgloss.Color("#FFFFFF"))
	switch method {
	case "GET":
		style = style.Background(lipgloss.Color("#4779c4"))
	case "POST":
		style = style.Background(lipgloss.Color("#6d8f56"))
	case "PUT":
		style = style.Background(lipgloss.Color("#d5a622"))
	case "PATCH":
		style = style.Background(lipgloss.Color("173"))
	case "DELETE":
		style = style.Background(lipgloss.Color("#c96169"))
	case "OPTIONS", "HEAD":
		style = style.Background(lipgloss.Color("240"))
	default:
		style = style.Background(lipgloss.Color("243"))
	}
	return style.Render(method)
}

func renderPathWithDimmedParams(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}

	var parts []string
	start := 0
	for index := 0; index < len(path); index++ {
		if path[index] != '{' {
			continue
		}
		end := strings.IndexByte(path[index:], '}')
		if end < 0 {
			break
		}
		end += index
		if index > start {
			parts = append(parts, pathBaseStyle.Render(path[start:index]))
		}
		paramName := strings.TrimSpace(path[index+1 : end])
		paramToken := ":"
		if paramName != "" {
			paramToken += paramName
		}
		parts = append(parts, pathParamStyle.Render(paramToken))
		start = end + 1
		index = end
	}
	if start < len(path) {
		parts = append(parts, pathBaseStyle.Render(path[start:]))
	}
	if len(parts) == 0 {
		return pathBaseStyle.Render(path)
	}
	return strings.Join(parts, "")
}

func (model *rootModel) availableResponseChips() ([]string, []string) {
	successCodes, errorCodes := model.availableResponseCodes()
	successChips := make([]string, 0, len(successCodes))
	errorChips := make([]string, 0, len(errorCodes))
	for _, code := range successCodes {
		successChips = append(successChips, responseSuccessStyle.Render(code))
	}
	for _, code := range errorCodes {
		errorChips = append(errorChips, responseErrorChipStyle.Render(code))
	}
	return successChips, errorChips
}

func (model *rootModel) availableResponseCodes() ([]string, []string) {
	if model.explorer.Detail.Operation == nil {
		return nil, nil
	}

	var operation map[string]json.RawMessage
	if err := json.Unmarshal(model.explorer.Detail.Operation.Body, &operation); err != nil {
		return nil, nil
	}
	responsesBody, ok := operation["responses"]
	if !ok {
		return nil, nil
	}

	var responses map[string]json.RawMessage
	if err := json.Unmarshal(responsesBody, &responses); err != nil {
		return nil, nil
	}
	if len(responses) == 0 {
		return nil, nil
	}

	codes := sortedResponseCodesForChips(responses)
	successCodes := make([]string, 0, len(codes))
	errorCodes := make([]string, 0, len(codes))
	for _, code := range codes {
		if is2xxStatusCode(code) {
			successCodes = append(successCodes, code)
			continue
		}
		errorCodes = append(errorCodes, code)
	}
	return successCodes, errorCodes
}

func sortedResponseCodesForChips(responses map[string]json.RawMessage) []string {
	type responseCode struct {
		code      string
		numeric   bool
		value     int
		isDefault bool
	}

	entries := make([]responseCode, 0, len(responses))
	for code := range responses {
		trimmed := strings.TrimSpace(code)
		value, err := strconv.Atoi(trimmed)
		entries = append(entries, responseCode{
			code:      trimmed,
			numeric:   err == nil,
			value:     value,
			isDefault: trimmed == "default",
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

func is2xxStatusCode(code string) bool {
	numeric, err := strconv.Atoi(strings.TrimSpace(code))
	if err != nil {
		return false
	}
	return numeric >= 200 && numeric < 300
}
