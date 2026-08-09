package model

import (
	"testing"
)

func TestStoredSiteGroupMultiplier(t *testing.T) {
	tests := []struct {
		name       string
		rawPayload string
		groupKey   string
		want       float64
		wantOK     bool
	}{
		{
			name:       "new api map ratio",
			rawPayload: `{"data":{"claude_code":{"name":"Claude Code","ratio":5},"complimentary":{"ratio":0}}}`,
			groupKey:   "claude_code",
			want:       5,
			wantOK:     true,
		},
		{
			name:       "explicit zero ratio",
			rawPayload: `{"data":{"claude_code":{"ratio":5},"complimentary":{"ratio":0}}}`,
			groupKey:   "complimentary",
			want:       0,
			wantOK:     true,
		},
		{
			name:       "sub2 list multiplier",
			rawPayload: `{"data":[{"id":21,"rate_multiplier":"0.06"}]}`,
			groupKey:   "21",
			want:       0.06,
			wantOK:     true,
		},
		{
			name:       "non numeric ratio remains unknown",
			rawPayload: `{"data":{"automatic":{"ratio":"auto"}}}`,
			groupKey:   "automatic",
			wantOK:     false,
		},
		{
			name:       "different group is not reused",
			rawPayload: `{"data":{"default":{"ratio":2}}}`,
			groupKey:   "claude_code",
			wantOK:     false,
		},
		{
			name:       "invalid json returns unknown",
			rawPayload: `{invalid json`,
			groupKey:   "claude_code",
			wantOK:     false,
		},
		{
			name:       "empty payload returns unknown",
			rawPayload: ``,
			groupKey:   "claude_code",
			wantOK:     false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := StoredSiteGroupMultiplier(test.rawPayload, test.groupKey)
			if ok != test.wantOK || got != test.want {
				t.Fatalf("StoredSiteGroupMultiplier() = (%v, %v), want (%v, %v)", got, ok, test.want, test.wantOK)
			}
		})
	}
}
