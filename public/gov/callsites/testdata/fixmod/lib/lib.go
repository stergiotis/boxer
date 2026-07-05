package lib

func SameModuleFunc() {
	helper() // @lib-internal
}

func helper() {}
