package collector

import (
	"testing"
)

func Test_sanitizeValue(t *testing.T) {
	type args struct {
		v any
	}
	tests := []struct {
		name    string
		args    args
		want    float64
		wantErr bool
	}{
		{"int", args{1}, 1.0, false},
		{"float", args{3.44}, 3.44, false},
		{"int-text", args{"1"}, 1.0, false},
		{"float-text", args{"3.44"}, 3.44, false},
		{"true", args{true}, 1, false},
		{"false", args{false}, 0, false},
		{"true-text", args{"true"}, 1, false},
		{"false-text", args{"false"}, 0, false},
		{"blah", args{"blah"}, 0, true},
		{"", args{""}, 0, true},
		{"neg_int", args{-1}, -1.0, false},
		{"neg_float", args{-3.44}, -3.44, false},
		{"neg_int-text", args{"-1"}, -1.0, false},
		{"neg_float-text", args{"-3.44"}, -3.44, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sanitizeValue(tt.args.v)
			if (err != nil) != tt.wantErr {
				t.Errorf("sanitizeValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("sanitizeValue() = %v, want %v", got, tt.want)
			}
		})
	}
}
