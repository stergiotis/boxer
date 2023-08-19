package logging

func ConvertStringKeyedMaps(val interface{}) interface{} {
	switch valt := val.(type) {
	case map[interface{}]interface{}:
		r := make(map[string]interface{}, len(valt))
		for k, v := range valt {
			ks, ok := k.(string)
			if !ok {
				return val
			}
			r[ks] = ConvertStringKeyedMaps(v)
		}
		return r
	case []interface{}:
		for i, v := range valt {
			valt[i] = ConvertStringKeyedMaps(v)
		}
		return valt
	}
	return val
}
