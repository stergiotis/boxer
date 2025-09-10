package naming

const InvalidEnumValueString = "<invalid>"
const (
	LowerCamelCase  NamingStyleE = 0
	UpperCamelCase  NamingStyleE = 1
	LowerSnakeCase  NamingStyleE = 2
	UpperSnakeCase  NamingStyleE = 3
	LowerSpinalCase NamingStyleE = 4
	UpperSpinalCase NamingStyleE = 5
)

var AllNamingStyles = []NamingStyleE{
	LowerCamelCase,
	UpperCamelCase,
	LowerSnakeCase,
	UpperSnakeCase,
	LowerSpinalCase,
	UpperSpinalCase,
}

func (inst NamingStyleE) String() string {
	switch inst {
	case LowerCamelCase:
		return "lowerCamelCase"
	case UpperCamelCase:
		return "UpperCamelCase"
	case LowerSnakeCase:
		return "lower_snake_case"
	case UpperSnakeCase:
		return "UPPER_SNAKE_CASE"
	case LowerSpinalCase:
		return "lower-spinal-case"
	case UpperSpinalCase:
		return "UPPER-SPINAL-CASE"
	}
	return InvalidEnumValueString
}
