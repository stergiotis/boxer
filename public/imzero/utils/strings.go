package utils

func TruncateDescriptiveNameLeft(name string, max int, ellipsis string) (r string) {
	l := len(name)
	if l <= max {
		r = name
		return
	}
	u := len(ellipsis)
	if u > max {
		r = ellipsis[:max]
		return
	}
	t := max - u
	r = ellipsis + name[l-t:]
	return
}
