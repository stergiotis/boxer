package eh

import "errors"

func AppendError(errsIn []error, err error) (errsOut []error) {
	if err == nil {
		errsOut = errsIn
		return
	}
	l := len(errsIn)
	if l == 0 {
		errsOut = append(errsIn, err)
		return
	}
	if errsIn[l-1] != err {
		errsOut = append(errsIn, err)
	}
	return
}
func CheckErrors(errs []error) (err error) {
	if len(errs) > 0 {
		err = errors.Join(errs...)
		return
	}
	return
}
func ClearErrors(errsIn []error) (errsOut []error) {
	clear(errsIn)
	errsOut = errsIn[:0]
	return
}
