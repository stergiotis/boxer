package widgets

import "testing"

func TestCvDroneDataBuilds(t *testing.T) {
	d := ensureCvDroneData()
	if d.err != "" {
		t.Fatalf("data build error: %s", d.err)
	}
	if d.rec == nil {
		t.Fatal("nil record")
	}
	t.Logf("rec rows=%d cols=%d sections=%d", d.rec.NumRows(), d.rec.Schema().NumFields(), len(d.tblDesc.TaggedValuesSections))
	drv, err := newCvCardDriver(d)
	if err != nil {
		t.Fatalf("driver build error: %v", err)
	}
	if drv == nil {
		t.Fatal("nil driver")
	}
}
