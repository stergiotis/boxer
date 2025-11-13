package fs

import (
	"sync"

	"io/fs"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

type BoundedFsWatcher struct {
	watcher   *fsnotify.Watcher
	lastError error
	filter    func(event fsnotify.Event) (keep bool)
	events    []fsnotify.Event
	cur       int
	open      bool
	mtx       sync.Mutex
}

func NewBoundedFsWatcher(maxEvents int, filter func(event fsnotify.Event) (keep bool)) (inst *BoundedFsWatcher, err error) {
	var watcher *fsnotify.Watcher
	watcher, err = fsnotify.NewWatcher()
	if err != nil {
		err = eh.Errorf("unable to create watcher: %w", err)
		return
	}
	if maxEvents <= 0 {
		err = eh.Errorf("maxEvents must be at least 1")
		return
	}

	inst = &BoundedFsWatcher{
		watcher:   watcher,
		lastError: nil,
		filter:    filter,
		events:    make([]fsnotify.Event, 0, maxEvents),
		cur:       0,
		open:      false,
		mtx:       sync.Mutex{},
	}
	inst.setup()
	return
}
func (inst *BoundedFsWatcher) IsOpen() bool {
	return inst.open
}
func (inst *BoundedFsWatcher) LastError() (err error) {
	return inst.lastError
}
func (inst *BoundedFsWatcher) GetMaxEvents() int {
	return cap(inst.events)
}
func (inst *BoundedFsWatcher) GetAndClearEvents(eventsIn []fsnotify.Event) (eventsOut []fsnotify.Event) {
	inst.mtx.Lock()
	defer inst.mtx.Unlock()
	events := inst.events
	l := len(events)
	if eventsIn == nil {
		eventsOut = make([]fsnotify.Event, 0, l)
	} else {
		eventsOut = eventsIn[:0]
	}
	cur := inst.cur
	for i := cur; i < l; i++ {
		eventsOut = append(eventsOut, events[i])
	}
	for i := 0; i < cur; i++ {
		eventsOut = append(eventsOut, events[i])
	}
	inst.cur = 0
	inst.events = events[:0]
	return
}
func (inst *BoundedFsWatcher) addEvent(ev fsnotify.Event) {
	inst.mtx.Lock()
	defer inst.mtx.Unlock()
	events := inst.events
	cur := inst.cur
	cur++
	if cur > cap(events) {
		cur = 0
	}
	if cur >= len(events) {
		events = append(events, ev)
		inst.events = events
	} else {
		events[cur] = ev
	}
}
func (inst *BoundedFsWatcher) setup() {
	watcher := inst.watcher
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					inst.lastError = eh.Errorf("unable to watch sql file dir, stopping watch")
					log.Info().Err(inst.lastError).Msg("error while watching fs")
					_ = inst.Close()
					return
				}
				if inst.filter(event) {
					inst.addEvent(event)
				}
			case e, ok := <-watcher.Errors:
				if !ok {
					inst.lastError = eh.Errorf("unable to watch sql file dir, stopping watch")
					log.Info().Err(inst.lastError).Msg("error while watching fs")
					_ = inst.Close()
					return
				}
				log.Warn().Err(e).Msg("watcher error")
			}
		}
	}()
	return
}
func (inst *BoundedFsWatcher) Close() (err error) {
	if inst.open {
		err = inst.watcher.Close()
		inst.open = false
		inst.cur = 0
		clear(inst.events)
		inst.events = inst.events[:0]
	}
	return
}

// AddDirRecursive uses fs.WalkDir and therefore does not follow symlinks
func (inst *BoundedFsWatcher) AddDirRecursive(root fs.FS, ignoreErrors bool, predicate func(path string, d fs.DirEntry) (prefixForLog string, add bool)) (err error) {
	open := inst.open
	watcher := inst.watcher
	statistics := make(map[string]uint64, 128)
	err = fs.WalkDir(root, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if ignoreErrors {
				log.Info().Err(err).Str("path", path).Msg("error while walking directory, skipping")
			} else {
				return err
			}
			return nil
		}
		if d.IsDir() {
			var b string
			add := true
			if predicate != nil {
				b, add = predicate(path, d)
			}
			if add {
				statistics[b] = statistics[b] + 1
				e := watcher.Add(path)
				if e != nil {
					if ignoreErrors {
						log.Info().Err(e).Msg("unable to watch directory, skipping")
					} else {
						return e
					}
				} else {
					open = true
				}
			}
		}
		return nil
	})
	if len(statistics) > 0 {
		log.Debug().Interface("statistics", statistics).Msg("added path(s) to watcher")
	}
	inst.open = open
	if err != nil {
		err = eh.Errorf("unable to walk director: %w", err)
		return
	}
	return
}
func (inst *BoundedFsWatcher) AddDir(path string) (err error) {
	err = inst.watcher.Add(path)
	if err != nil {
		err = eh.Errorf("unable to watch directory: %w", err)
		return
	}
	inst.open = true
	return
}
func (inst *BoundedFsWatcher) ResetWatches() (err error) {
	w := inst.watcher
	l := w.WatchList()
	nErr := 0
	for _, p := range l {
		err = w.Remove(p)
		if err != nil {
			nErr++
		}
	}
	if nErr > 0 {
		err = eb.Build().Int("nErrors", nErr).Int("nWatches", len(l)).Errorf("unable to remove all watches")
	}
	return
}
