package rt

import "testing"

func TestJSStringUTF16Semantics(t *testing.T) {
	if n := JSLength("é"); n != 1 {
		t.Errorf("length(é) = %d", n)
	}
	if n := JSLength("😀"); n != 2 {
		t.Errorf("length(😀) = %d", n)
	}
	s := "a😀b"
	want := []int32{'a', 0xD83D, 0xDE00, 'b'}
	for i, w := range want {
		if c := JSCharCodeAt(s, int32(i)); c != w {
			t.Errorf("charCodeAt(%d) = %#x want %#x", i, c, w)
		}
	}
	if c := JSCodePointAt(s, 1); c != 0x1F600 {
		t.Errorf("codePointAt = %#x", c)
	}
	// substring splitting a surrogate pair yields lone surrogates (WTF-8)
	hi := JSSubstring(s, 1, 2).(string)
	lo := JSSubstring(s, 2, 3).(string)
	if JSLength(hi) != 1 || JSCharCodeAt(hi, 0) != 0xD83D || JSCharCodeAt(lo, 0) != 0xDE00 {
		t.Errorf("lone surrogates: %q %q", hi, lo)
	}
	// concatenating the halves re-forms the pair and equals the original
	if j := JSConcat(hi, lo); JSEquals(j, "😀") != 1 {
		t.Errorf("concat of surrogate halves = %q", j)
	}
	if JSCompare("a", "b") != -1 || JSCompare("b", "a") != 1 || JSCompare("x", "x") != 0 {
		t.Error("compare")
	}
	// UTF-16 order: U+FFFF sorts after U+10000 (whose first unit is 0xD800)
	if JSCompare("￿", "𐀀") != 1 {
		t.Error("compare UTF-16 order")
	}
	arr := make([]int16, 4)
	if n := JSIntoCharCodeArray(s, arr, 0); n != 4 || uint16(arr[1]) != 0xD83D {
		t.Errorf("intoCharCodeArray: %d %v", n, arr)
	}
	if r := JSFromCharCodeArray(arr, 0, 4); r != s {
		t.Errorf("fromCharCodeArray = %q", r)
	}
	if JSTest(42) != 0 || JSTest("x") != 1 {
		t.Error("test")
	}
	if JSSubstring("hello", 3, 1) != "" || JSSubstring("hello", 1, 100) != "ello" {
		t.Error("substring clamping")
	}
}

func TestJSTextCodec(t *testing.T) {
	b := []int8{'h', 'i', -1, 'x'}
	if s := JSDecodeUTF8(b, 0, 4); s != "hi�x" {
		t.Errorf("decode = %q", s)
	}
	lone := JSSubstring("😀", 0, 1)
	if n := JSMeasureUTF8(lone); n != 3 {
		t.Errorf("measure lone surrogate = %d", n)
	}
	enc := JSEncodeUTF8(lone)
	if len(enc) != 3 || uint8(enc[0]) != 0xEF {
		t.Errorf("lone surrogate should encode as U+FFFD: %v", enc)
	}
}
