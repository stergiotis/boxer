package progressbar

import "io"

// ProxyReader wraps an io.Reader, Add-ing the byte count of each successful
// Read to the Bar. The common use is driving a byte-scale determinate bar
// from an HTTP response body, a file read, or a compressor stream:
//
//	bar := progressbar.New(resp.ContentLength, "bytes")
//	bar.Start(ctx)
//	defer bar.Stop()
//	pr := bar.NewProxyReader(resp.Body)
//	defer pr.Close()
//	_, _ = io.Copy(dst, pr)
//
// Close is propagated to the wrapped reader when it implements io.Closer.
type ProxyReader struct {
	r   io.Reader
	bar *Bar
}

// NewProxyReader wraps r with byte-count ticking against this Bar.
func (inst *Bar) NewProxyReader(r io.Reader) (pr *ProxyReader) {
	return &ProxyReader{r: r, bar: inst}
}

func (inst *ProxyReader) Read(p []byte) (n int, err error) {
	n, err = inst.r.Read(p)
	if n > 0 {
		inst.bar.Add(int64(n))
	}
	return
}

// Close propagates to the wrapped reader if it is an io.Closer; returns nil
// otherwise so the ProxyReader can be used in a deferred Close regardless.
func (inst *ProxyReader) Close() (err error) {
	if c, ok := inst.r.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// ProxyWriter is the write-side counterpart to ProxyReader. Each successful
// Write adds its byte count to the Bar.
type ProxyWriter struct {
	w   io.Writer
	bar *Bar
}

// NewProxyWriter wraps w with byte-count ticking against this Bar.
func (inst *Bar) NewProxyWriter(w io.Writer) (pw *ProxyWriter) {
	return &ProxyWriter{w: w, bar: inst}
}

func (inst *ProxyWriter) Write(p []byte) (n int, err error) {
	n, err = inst.w.Write(p)
	if n > 0 {
		inst.bar.Add(int64(n))
	}
	return
}

func (inst *ProxyWriter) Close() (err error) {
	if c, ok := inst.w.(io.Closer); ok {
		return c.Close()
	}
	return nil
}
