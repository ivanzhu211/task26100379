package ledger

import (
	"reflect"
	"testing"
)

func TestEmptyLedger(t *testing.T) {
	l := New()
	if got := l.Ranges(0, Blocks); len(got) != 0 {
		t.Fatalf("empty ledger has ranges: %v", got)
	}
}

func TestMarkVersionsIncrease(t *testing.T) {
	l := New()
	v1 := l.Mark(0, 10)
	v2 := l.Mark(0, 10) // re-marking still bumps the version
	v3 := l.Mark(5, 20)
	if !(v1 < v2 && v2 < v3) {
		t.Fatalf("versions not increasing: %d %d %d", v1, v2, v3)
	}
	want := []Range{{0, 5, v2}, {5, 20, v3}}
	if got := l.Ranges(0, Blocks); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestCaptureDoesNotClear(t *testing.T) {
	l := New()
	v := l.Mark(10, 20)
	_, rs := l.Capture(0, Blocks)
	want := []Range{{10, 20, v}}
	if !reflect.DeepEqual(rs, want) {
		t.Fatalf("capture got %v want %v", rs, want)
	}
	if got := l.Ranges(0, Blocks); !reflect.DeepEqual(got, want) {
		t.Fatalf("capture cleared state: %v", got)
	}
}

func TestFinishFailureKeepsDirty(t *testing.T) {
	l := New()
	v := l.Mark(0, 100)
	tk, _ := l.Capture(0, Blocks)
	l.Finish(tk, false)
	want := []Range{{0, 100, v}}
	if got := l.Ranges(0, Blocks); !reflect.DeepEqual(got, want) {
		t.Fatalf("failed finish cleared state: %v", got)
	}
}

func TestRepeatFinishIsNoop(t *testing.T) {
	l := New()
	v := l.Mark(0, 100)
	tk, _ := l.Capture(0, Blocks)
	l.Finish(tk, false) // first finish: invalidates, keeps dirty
	l.Finish(tk, true)  // must be a no-op
	want := []Range{{0, 100, v}}
	if got := l.Ranges(0, Blocks); !reflect.DeepEqual(got, want) {
		t.Fatalf("repeat finish changed state: %v", got)
	}
	l.Finish(tk, true)
	l.Finish(Ticket(9999), true) // unknown ticket
	if got := l.Ranges(0, Blocks); !reflect.DeepEqual(got, want) {
		t.Fatalf("later repeat finish changed state: %v", got)
	}
}

func TestSuccessOnlyConfirmsCapturedVersion(t *testing.T) {
	l := New()
	v1 := l.Mark(0, 100)
	tk, _ := l.Capture(0, Blocks)
	v2 := l.Mark(40, 60) // rewritten after capture
	l.Finish(tk, true)
	want := []Range{{40, 60, v2}}
	if got := l.Ranges(0, Blocks); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v (v1=%d v2=%d)", got, want, v1, v2)
	}
}

func TestCleanHoleAtCaptureStaysDirty(t *testing.T) {
	l := New()
	v1 := l.Mark(0, 10)
	v1b := l.Mark(90, 100)
	tk, _ := l.Capture(0, Blocks) // hole (10,90) is clean at capture
	v2 := l.Mark(40, 50)          // hole written after capture
	l.Finish(tk, true)
	want := []Range{{40, 50, v2}}
	if got := l.Ranges(0, Blocks); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v (v1=%d v1b=%d v2=%d)", got, want, v1, v1b, v2)
	}
}

func TestClearedGapStaysClean(t *testing.T) {
	l := New()
	v1 := l.Mark(0, 30)
	v2 := l.Mark(10, 20) // splits v1 into two pieces
	tk, _ := l.Capture(10, 20)
	l.Finish(tk, true) // clears v2; the gap must stay clean, no merge
	want := []Range{{0, 10, v1}, {20, 30, v1}}
	if got := l.Ranges(0, Blocks); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v (v1=%d v2=%d)", got, want, v1, v2)
	}
}

func TestAdjacentDifferentVersionNotMerged(t *testing.T) {
	l := New()
	v1 := l.Mark(0, 10)
	v2 := l.Mark(10, 20)
	want := []Range{{0, 10, v1}, {10, 20, v2}}
	if got := l.Ranges(0, Blocks); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestRangesWindowClips(t *testing.T) {
	l := New()
	v := l.Mark(10, 90)
	want := []Range{{20, 50, v}}
	if got := l.Ranges(20, 50); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if got := l.Ranges(0, 10); len(got) != 0 {
		t.Fatalf("got %v want empty", got)
	}
}

// TestTrillionBlocks marks a trillion-block range, captures it, then
// rewrites three middle blocks. The old ticket's success must leave
// exactly those three new-version blocks dirty, without ever
// expanding the range block by block.
func TestTrillionBlocks(t *testing.T) {
	l := New()
	const trillion = uint64(1) << 40 // 1,099,511,627,776 blocks
	v1 := l.Mark(0, trillion)
	tk, rs := l.Capture(0, trillion)
	if want := []Range{{0, trillion, v1}}; !reflect.DeepEqual(rs, want) {
		t.Fatalf("capture got %v want %v", rs, want)
	}
	mid := trillion / 2
	v2 := l.Mark(mid, mid+3)
	l.Finish(tk, true)
	want := []Range{{mid, mid + 3, v2}}
	if got := l.Ranges(0, Blocks); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if l.size != 1 {
		t.Fatalf("ledger holds %d intervals, want 1 (no block expansion)", l.size)
	}
}
