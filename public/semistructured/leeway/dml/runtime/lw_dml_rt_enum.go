package runtime

const (
	EntityStateInitial     EntityStateE = 0
	EntityStateInEntity    EntityStateE = 1
	EntityStateInSection   EntityStateE = 2
	EntityStateInAttribute EntityStateE = 3
)
const InvalidEnumString = "<invalid>"

var EntityStateVariableNames = [4]string{
	"EntityStateInitial",
	"EntityStateInEntity",
	"EntityStateInSection",
	"EntityStateInAttribute",
}

func (inst EntityStateE) String() string {
	switch inst {
	case EntityStateInitial:
		return "initial"
	case EntityStateInEntity:
		return "in-entity"
	case EntityStateInSection:
		return "in-section"
	case EntityStateInAttribute:
		return "in-attribute"
	}
	return InvalidEnumString
}
