package naming

const InvalidEnumValueString = "<invalid>"
const (
	NamingStyleLowerCamelCase NamingStyleE = 0
	NamingStyleUpperCamelCase NamingStyleE = 1
	NamingStyleSnakeCase      NamingStyleE = 2
	NamingStyleSpinalCase     NamingStyleE = 3
)

var AllNamingStyles = []NamingStyleE{
	NamingStyleLowerCamelCase,
	NamingStyleUpperCamelCase,
	NamingStyleSnakeCase,
	NamingStyleSpinalCase,
}

func (inst NamingStyleE) String() string {
	switch inst {
	case NamingStyleLowerCamelCase:
		return "lowerCamelCase"
	case NamingStyleUpperCamelCase:
		return "UpperCamelCase"
	case NamingStyleSnakeCase:
		return "snake_case"
	case NamingStyleSpinalCase:
		return "spinal-case"
	}
	return InvalidEnumValueString
}
