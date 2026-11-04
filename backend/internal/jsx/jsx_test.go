package jsx

import (
	"math"
	"testing"
)

func TestNumberString(t *testing.T) {
	for in, want := range map[float64]string{
		0: "0", 3: "3", -1.5: "-1.5", 12500: "12500", 2.67: "2.67", 1e21: "1e+21", 1.5e-7: "1.5e-7",
		0.000001: "0.000001", 123456789012345680000: "123456789012345680000", math.Inf(1): "Infinity",
	} {
		if got := NumberString(in); got != want {
			t.Errorf("NumberString(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestToNumber(t *testing.T) {
	for in, want := range map[string]float64{
		"": 0, " 3 ": 3, "3.5": 3.5, ".5": 0.5, "5.": 5, "+4": 4, "-0": 0, "1e3": 1000, "0x10": 16, "0b11": 3, "0o7": 7,
		"Infinity": math.Inf(1), "-Infinity": math.Inf(-1),
	} {
		if got := ToNumber(in); got != want {
			t.Errorf("ToNumber(%q) = %v, want %v", in, got, want)
		}
	}
	for _, in := range []string{"abc", "1,5", "-0x10", "0x", "1_000", "inf", "0x1p3", "1e", "."} {
		if got := ToNumber(in); !math.IsNaN(got) {
			t.Errorf("ToNumber(%q) = %v, want NaN", in, got)
		}
	}
}

func TestToFixed2(t *testing.T) {
	for in, want := range map[float64]string{0: "0.00", 3: "3.00", 3.455: "3.46", 0.125: "0.13", 2.675: "2.67", 1.005: "1.00", 4: "4.00"} {
		if got := ToFixed2(in); got != want {
			t.Errorf("ToFixed2(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestUTF16(t *testing.T) {
	if n := UTF16Len("a😀я"); n != 4 {
		t.Fatal(n)
	}
	if s := UTF16Prefix("a😀я", 2); s != "a" {
		t.Fatalf("%q", s)
	}
	if s := UTF16Prefix("a😀я", 3); s != "a😀" {
		t.Fatalf("%q", s)
	}
}
