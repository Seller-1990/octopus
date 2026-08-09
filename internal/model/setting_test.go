package model

import "testing"

func TestSettingValidateDefaultMultiplierCap(t *testing.T) {
	valid := []string{"0", "4", "0.25", "1e3"}
	for _, value := range valid {
		if err := (&Setting{Key: SettingKeyDefaultMultiplierCap, Value: value}).Validate(); err != nil {
			t.Fatalf("cap %q rejected: %v", value, err)
		}
	}
	invalid := []string{"-1", "NaN", "+Inf", "not-a-number"}
	for _, value := range invalid {
		if err := (&Setting{Key: SettingKeyDefaultMultiplierCap, Value: value}).Validate(); err == nil {
			t.Fatalf("cap %q was accepted", value)
		}
	}
}
