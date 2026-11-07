package good

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// The mount context supplies identity, instance key and lifetime.
func viaForApp(ctx app.MountContextI) task.TaskApiI {
	return task.ForApp(ctx)
}

// A local function with the same name is not the task package's.
func NewBusApi(bus app.BusI) app.BusI { return bus }

func viaLocal(bus app.BusI) app.BusI {
	return NewBusApi(bus)
}
