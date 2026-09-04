package handlers

import "testing"

func TestMaskSecret(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"empty stays empty", "", ""},
		{"short fully masked", "abc123", "****"},
		{"exactly 8 chars fully masked", "12345678", "****"},
		{"9 chars keeps head and tail", "123456789", "1234****6789"},
		{"long key masked in middle", "sk-octopus-abc123xyz", "sk-o****3xyz"},
		{"multi-byte runes not split", "密码测试数据十六进制", "密码测试****十六进制"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := maskSecret(tc.value); got != tc.want {
				t.Fatalf("maskSecret(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// 还原逻辑的判定契约：restore/update 只在「新值 == maskSecret(数据库旧值)」时
// 还原，因此真实新值即便包含 **** 也不会被误还原。
func TestMaskedFormIsNotConfusableWithRealValues(t *testing.T) {
	current := "correct-horse"
	newValue := "pass****word"
	if newValue == maskSecret(current) {
		t.Fatal("a real new value must not equal the masked form of the stored value, otherwise restore would revert it")
	}
	if isMaskedValue(newValue) != true {
		t.Fatal("create path must reject values containing the mask marker")
	}
	if maskSecret(current) == current {
		t.Fatal("masked form must differ from the plaintext it masks")
	}
}

func TestIsMaskedValue(t *testing.T) {
	if isMaskedValue("") {
		t.Fatal("empty value must not be treated as masked")
	}
	if isMaskedValue("sk-octopus-real-secret") {
		t.Fatal("plaintext credential must not be treated as masked")
	}
	for _, value := range []string{"****", "sk-o****3xyz", "abc****"} {
		if !isMaskedValue(value) {
			t.Fatalf("value %q containing mask marker should be recognized as masked", value)
		}
	}
}
