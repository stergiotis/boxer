package bad

type Reader interface { // want CS005 here
	Read(p []byte) (n int, err error)
}

type writer interface { // want CS005 here
	Write(p []byte) (n int, err error)
}

type ParserAdapter interface { // want CS005 here
	Parse(s string) (err error)
}

type Suppressed interface { //boxer:lint disable=CS005 reason="testdata coverage of suppression"
	Do()
}
