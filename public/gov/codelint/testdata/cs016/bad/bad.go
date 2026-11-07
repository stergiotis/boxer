package bad

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// The bus-only constructor, called.
func viaCall(bus app.BusI) task.TaskApiI {
	return task.NewBusApi(task.ApiConfig{Bus: bus}) // want a CS016 finding here
}

// The constructor taken as a value.
func viaValue() func(task.ApiConfig) *task.BusApi {
	return task.NewBusApi // want a CS016 finding here
}
