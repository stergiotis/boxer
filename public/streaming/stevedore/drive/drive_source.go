package drive

import (
	"bufio"
	"context"
	"io"
	"io/fs"
	"iter"
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/streaming/stevedore"
)

// SourceI yields the requests of one run, in an order that repeats across
// runs so a checkpoint means the same thing twice. A request's Origin is
// what its file reference hashes, so a source names it by something the
// next run names the same way: a path, a line number, a position.
type SourceI interface {
	// Name is the source's name for dead-letter rows, where it stands in
	// for a topic.
	Name() string
	// All yields the requests. A yielded error ends the run; a request the
	// source could not read is yielded with the error instead, so it
	// becomes a dead letter and the run goes on.
	All(ctx context.Context) iter.Seq2[stevedore.Request, error]
}

// Tree yields one request per regular file under Root of FS, in the
// lexical order fs.WalkDir walks, with the file's path as the origin. A
// file above MaxBody is yielded as an error, not read.
type Tree struct {
	FS   fs.FS
	Root string
	Hint string
	// MaxBody bounds a file's size; zero is unbounded.
	MaxBody int64
}

var _ SourceI = Tree{}

// Name implements SourceI.
func (inst Tree) Name() string { return "tree:" + inst.root() }

func (inst Tree) root() string {
	if inst.Root == "" {
		return "."
	}
	return inst.Root
}

// All implements SourceI.
func (inst Tree) All(ctx context.Context) iter.Seq2[stevedore.Request, error] {
	return func(yield func(stevedore.Request, error) bool) {
		_ = fs.WalkDir(inst.FS, inst.root(), func(path string, d fs.DirEntry, werr error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			req := stevedore.Request{Origin: path, Hint: inst.Hint}
			if werr != nil {
				if !yield(req, stevedore.Permanent(eh.Errorf("walk: %w", werr))) {
					return fs.SkipAll
				}
				return nil
			}
			if d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			info, ierr := d.Info()
			if ierr != nil {
				if !yield(req, stevedore.Permanent(eh.Errorf("stat: %w", ierr))) {
					return fs.SkipAll
				}
				return nil
			}
			if inst.MaxBody > 0 && info.Size() > inst.MaxBody {
				err := stevedore.Permanent(eb.Build().Int64("size", info.Size()).Int64("max", inst.MaxBody).Errorf("file exceeds the body bound"))
				if !yield(req, err) {
					return fs.SkipAll
				}
				return nil
			}
			body, rerr := fs.ReadFile(inst.FS, path)
			if rerr != nil {
				if !yield(req, stevedore.Permanent(eh.Errorf("read: %w", rerr))) {
					return fs.SkipAll
				}
				return nil
			}
			req.Body = body
			if !yield(req, nil) {
				return fs.SkipAll
			}
			return nil
		})
	}
}

// Lines yields one request per line of R, the newline removed, with the
// origin `<Origin>:<line number>` from one. A line above MaxLine ends the
// run with an error, because the rest of the stream cannot be found again.
type Lines struct {
	R      io.Reader
	Origin string
	Hint   string
	// MaxLine bounds a line; zero takes bufio's default token size.
	MaxLine int
}

var _ SourceI = Lines{}

// Name implements SourceI.
func (inst Lines) Name() string { return "lines:" + inst.Origin }

// All implements SourceI.
func (inst Lines) All(ctx context.Context) iter.Seq2[stevedore.Request, error] {
	return func(yield func(stevedore.Request, error) bool) {
		sc := bufio.NewScanner(inst.R)
		if inst.MaxLine > 0 {
			sc.Buffer(nil, inst.MaxLine)
		}
		n := uint64(0)
		for sc.Scan() {
			if ctx.Err() != nil {
				yield(stevedore.Request{}, ctx.Err())
				return
			}
			n++
			line := sc.Bytes()
			body := make([]byte, len(line))
			copy(body, line)
			req := stevedore.Request{Origin: inst.Origin + ":" + strconv.FormatUint(n, 10), Hint: inst.Hint, Body: body}
			if !yield(req, nil) {
				return
			}
		}
		if err := sc.Err(); err != nil {
			yield(stevedore.Request{}, eh.Errorf("read lines: %w", err))
		}
	}
}

// List yields the requests it holds, as they are.
type List struct {
	Requests []stevedore.Request
}

var _ SourceI = List{}

// Name implements SourceI.
func (inst List) Name() string { return "list" }

// All implements SourceI.
func (inst List) All(ctx context.Context) iter.Seq2[stevedore.Request, error] {
	return func(yield func(stevedore.Request, error) bool) {
		for _, req := range inst.Requests {
			if ctx.Err() != nil {
				yield(stevedore.Request{}, ctx.Err())
				return
			}
			if !yield(req, nil) {
				return
			}
		}
	}
}
