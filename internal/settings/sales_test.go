package settings

import (
	"strings"
	"testing"
)

func TestSalesValidation(t *testing.T) {
	v := Sales{ValidityDays: 30, Terms: "  បង់ប្រាក់ក្នុងរយៈពេល 30 ថ្ងៃ\nPayment due in 30 days.  ", Version: 1}
	if e := ValidateSales(&v); e != nil || strings.HasSuffix(v.Terms, " ") {
		t.Fatal(e)
	}
	for _, bad := range []Sales{{ValidityDays: 0, Version: 1}, {ValidityDays: 366, Version: 1}, {ValidityDays: 30, Version: 0}, {ValidityDays: 30, Version: 1, Terms: "bad\x00text"}, {ValidityDays: 30, Version: 1, Terms: strings.Repeat("x", 10001)}} {
		if ValidateSales(&bad) == nil {
			t.Fatal("invalid defaults accepted")
		}
	}
}
