package webhook

import "testing"

func Test_validateConfig(t *testing.T) {
	type args struct {
		config Config
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			"empty_path",
			args{Config{WebHooks: map[string]*WebHook{
				"test1": {Path: ""},
			}}},
			true,
		}, {
			"with_out_/",
			args{Config{WebHooks: map[string]*WebHook{
				"test1": {Path: "path"},
			}}},
			true,
		},
		{
			"valid",
			args{Config{WebHooks: map[string]*WebHook{
				"test1": {Path: "/wh1"},
				"test2": {Path: "/wh2"},
				"test3": {Path: "/path/wh3"},
			}}},
			false,
		},
		{
			"duplicate_paths",
			args{Config{WebHooks: map[string]*WebHook{
				"test1": {Path: "/wh"},
				"test2": {Path: "/wh"},
				"test3": {Path: "/path/wh3"},
			}}},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateConfig(tt.args.config); (err != nil) != tt.wantErr {
				t.Errorf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
