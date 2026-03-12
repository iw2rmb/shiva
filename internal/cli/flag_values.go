package cli

type stringFlagValue struct {
	target   *string
	typeName string
}

func newStringFlagValue(target *string, typeName string) *stringFlagValue {
	return &stringFlagValue{
		target:   target,
		typeName: typeName,
	}
}

func (v *stringFlagValue) String() string {
	if v == nil || v.target == nil {
		return ""
	}
	return *v.target
}

func (v *stringFlagValue) Set(value string) error {
	if v == nil || v.target == nil {
		return nil
	}
	*v.target = value
	return nil
}

func (v *stringFlagValue) Type() string {
	if v == nil {
		return "string"
	}
	return v.typeName
}
