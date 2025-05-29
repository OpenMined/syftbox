package utils

import (
	"testing"
)

func TestFromSyftURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    *SyftBoxURL
		wantErr bool
	}{
		{
			name: "valid basic url",
			url:  "syft://datasite1/app_data/app1/rpc/endpoint1",
			want: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "app1",
				Endpoint: "endpoint1",
			},
			wantErr: false,
		},
		{
			name: "valid url with query params",
			url:  "syft://datasite1/app_data/app1/rpc/endpoint1?param1=value1&param2=value2",
			want: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "app1",
				Endpoint: "endpoint1",
				queryParams: map[string]string{
					"param1": "value1",
					"param2": "value2",
				},
			},
			wantErr: false,
		},
		{
			name:    "invalid scheme",
			url:     "http://datasite1/app_data/app1/rpc/endpoint1",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "missing app_data",
			url:     "syft://datasite1/wrong/app1/rpc/endpoint1",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "missing rpc",
			url:     "syft://datasite1/app_data/app1/wrong/endpoint1",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "empty url",
			url:     "",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "malformed url",
			url:     "syft:///invalid",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromSyftURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("FromSyftURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.Datasite != tt.want.Datasite {
					t.Errorf("FromSyftURL() Datasite = %v, want %v", got.Datasite, tt.want.Datasite)
				}
				if got.AppName != tt.want.AppName {
					t.Errorf("FromSyftURL() AppName = %v, want %v", got.AppName, tt.want.AppName)
				}
				if got.Endpoint != tt.want.Endpoint {
					t.Errorf("FromSyftURL() Endpoint = %v, want %v", got.Endpoint, tt.want.Endpoint)
				}
				if tt.want.queryParams != nil {
					for k, v := range tt.want.queryParams {
						if gotVal, exists := got.queryParams[k]; !exists || gotVal != v {
							t.Errorf("FromSyftURL() queryParams[%s] = %v, want %v", k, gotVal, v)
						}
					}
				}
			}
		})
	}
}

func TestFromSyftURL_QueryParamEncoding(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    *SyftBoxURL
		wantErr bool
	}{
		{
			name: "url with encoded spaces in query param values",
			url:  "syft://datasite1/app_data/app1/rpc/endpoint1?param1=value%20with%20spaces&param2=value2",
			want: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "app1",
				Endpoint: "endpoint1",
				queryParams: map[string]string{
					"param1": "value with spaces",
					"param2": "value2",
				},
			},
			wantErr: false,
		},
		{
			name: "url with encoded special chars in query param values",
			url:  "syft://datasite1/app_data/app1/rpc/endpoint1?param1=value%26with%26chars",
			want: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "app1",
				Endpoint: "endpoint1",
				queryParams: map[string]string{
					"param1": "value&with&chars",
				},
			},
			wantErr: false,
		},
		{
			name:    "url with spaces in query param keys",
			url:     "syft://datasite1/app_data/app1/rpc/endpoint1?param with spaces=value1",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "url with special chars in query param keys",
			url:     "syft://datasite1/app_data/app1/rpc/endpoint1?param%26with%26chars=value1",
			want:    nil,
			wantErr: true,
		},
		{
			name: "url with multiple values for same key",
			url:  "syft://datasite1/app_data/app1/rpc/endpoint1?param1=value1&param1=value2",
			want: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "app1",
				Endpoint: "endpoint1",
				queryParams: map[string]string{
					"param1": "value1", // First value should be used
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromSyftURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("FromSyftURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.Datasite != tt.want.Datasite {
					t.Errorf("FromSyftURL() Datasite = %v, want %v", got.Datasite, tt.want.Datasite)
				}
				if got.AppName != tt.want.AppName {
					t.Errorf("FromSyftURL() AppName = %v, want %v", got.AppName, tt.want.AppName)
				}
				if got.Endpoint != tt.want.Endpoint {
					t.Errorf("FromSyftURL() Endpoint = %v, want %v", got.Endpoint, tt.want.Endpoint)
				}
				if tt.want.queryParams != nil {
					for k, v := range tt.want.queryParams {
						if gotVal, exists := got.queryParams[k]; !exists || gotVal != v {
							t.Errorf("FromSyftURL() queryParams[%s] = %v, want %v", k, gotVal, v)
						}
					}
				}
			}
		})
	}
}

func TestSyftBoxURL_String(t *testing.T) {
	tests := []struct {
		name string
		url  *SyftBoxURL
		want string
	}{
		{
			name: "basic url",
			url: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "app1",
				Endpoint: "endpoint1",
			},
			want: "syft://datasite1/app_data/app1/rpc/endpoint1",
		},
		{
			name: "url with query params",
			url: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "app1",
				Endpoint: "endpoint1",
				queryParams: map[string]string{
					"param1": "value1",
					"param2": "value2",
				},
			},
			want: "syft://datasite1/app_data/app1/rpc/endpoint1?param1=value1&param2=value2",
		},
		{
			name: "url with spaces in query param values",
			url: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "app1",
				Endpoint: "endpoint1",
				queryParams: map[string]string{
					"param1": "value with spaces",
				},
			},
			want: "syft://datasite1/app_data/app1/rpc/endpoint1?param1=value+with+spaces",
		},
		{
			name: "url with special chars in query param values",
			url: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "app1",
				Endpoint: "endpoint1",
				queryParams: map[string]string{
					"param1": "value&with&chars",
				},
			},
			want: "syft://datasite1/app_data/app1/rpc/endpoint1?param1=value%26with%26chars",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.url.String(); got != tt.want {
				t.Errorf("SyftBoxURL.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSyftBoxURL_ToLocalPath(t *testing.T) {
	tests := []struct {
		name string
		url  *SyftBoxURL
		want string
	}{
		{
			name: "basic path",
			url: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "app1",
				Endpoint: "endpoint1",
			},
			want: "datasite1/app_data/app1/rpc/endpoint1",
		},
		{
			name: "path with nested endpoint",
			url: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "app1",
				Endpoint: "endpoint1/sub/path",
			},
			want: "datasite1/app_data/app1/rpc/endpoint1/sub/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.url.ToLocalPath(); got != tt.want {
				t.Errorf("SyftBoxURL.ToLocalPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSyftBoxURL_Validate(t *testing.T) {
	tests := []struct {
		name    string
		url     *SyftBoxURL
		wantErr bool
	}{
		{
			name: "valid url",
			url: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "app1",
				Endpoint: "endpoint1",
			},
			wantErr: false,
		},
		{
			name: "empty datasite",
			url: &SyftBoxURL{
				Datasite: "",
				AppName:  "app1",
				Endpoint: "endpoint1",
			},
			wantErr: true,
		},
		{
			name: "empty app name",
			url: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "",
				Endpoint: "endpoint1",
			},
			wantErr: true,
		},
		{
			name: "empty endpoint",
			url: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "app1",
				Endpoint: "",
			},
			wantErr: true,
		},
		{
			name: "endpoint with spaces",
			url: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "app1",
				Endpoint: "endpoint with spaces",
			},
			wantErr: true,
		},
		{
			name: "endpoint with special chars",
			url: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "app1",
				Endpoint: "endpoint?with=chars",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.url.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("SyftBoxURL.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				validationErr, ok := err.(*ValidationError)
				if !ok {
					t.Errorf("expected ValidationError, got %T", err)
				}
				if validationErr.Field == "" {
					t.Error("expected non-empty field in ValidationError")
				}
			}
		})
	}
}

func TestSyftBoxURL_QueryParams(t *testing.T) {
	tests := []struct {
		name string
		url  *SyftBoxURL
		want map[string]string
	}{
		{
			name: "with query params",
			url: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "app1",
				Endpoint: "endpoint1",
				queryParams: map[string]string{
					"param1": "value1",
					"param2": "value2",
				},
			},
			want: map[string]string{
				"param1": "value1",
				"param2": "value2",
			},
		},
		{
			name: "nil query params",
			url: &SyftBoxURL{
				Datasite: "datasite1",
				AppName:  "app1",
				Endpoint: "endpoint1",
			},
			want: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.url.QueryParams()
			if len(got) != len(tt.want) {
				t.Errorf("SyftBoxURL.QueryParams() length = %v, want %v", len(got), len(tt.want))
				return
			}
			for k, v := range tt.want {
				if gotVal, exists := got[k]; !exists || gotVal != v {
					t.Errorf("SyftBoxURL.QueryParams()[%s] = %v, want %v", k, gotVal, v)
				}
			}
		})
	}
}
