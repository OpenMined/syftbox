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
			url:  "syft://user@example.com/app_data/app1/rpc/endpoint1",
			want: &SyftBoxURL{
				Datasite: "user@example.com",
				AppName:  "app1",
				Endpoint: "endpoint1",
			},
			wantErr: false,
		},
		{
			name: "valid url with query params",
			url:  "syft://user@example.com/app_data/app1/rpc/endpoint1?param1=value1&param2=value2",
			want: &SyftBoxURL{
				Datasite: "user@example.com",
				AppName:  "app1",
				Endpoint: "endpoint1",
				QueryParams: map[string]string{
					"param1": "value1",
					"param2": "value2",
				},
			},
			wantErr: false,
		},
		{
			name:    "invalid scheme",
			url:     "http://user@example.com/app_data/app1/rpc/endpoint1",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "missing app_data",
			url:     "syft://user@example.com/wrong/app1/rpc/endpoint1",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "missing rpc",
			url:     "syft://user@example.com/app_data/app1/wrong/endpoint1",
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
		{
			name:    "invalid datasite format",
			url:     "syft://notanemail/app_data/app1/rpc/endpoint1",
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
				if len(tt.want.QueryParams) > 0 {
					if len(got.QueryParams) != len(tt.want.QueryParams) {
						t.Errorf("FromSyftURL() QueryParams length = %v, want %v", len(got.QueryParams), len(tt.want.QueryParams))
					}
					for k, v := range tt.want.QueryParams {
						if gotVal, exists := got.QueryParams[k]; !exists || gotVal != v {
							t.Errorf("FromSyftURL() QueryParams[%s] = %v, want %v", k, gotVal, v)
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
			url:  "syft://test@example.com/app_data/app1/rpc/endpoint1?param1=value%20with%20spaces&param2=value2",
			want: &SyftBoxURL{
				Datasite: "test@example.com",
				AppName:  "app1",
				Endpoint: "endpoint1",
				QueryParams: map[string]string{
					"param1": "value with spaces",
					"param2": "value2",
				},
			},
			wantErr: false,
		},
		{
			name: "url with encoded special chars in query param values",
			url:  "syft://test@example.com/app_data/app1/rpc/endpoint1?param1=value%26with%26chars",
			want: &SyftBoxURL{
				Datasite: "test@example.com",
				AppName:  "app1",
				Endpoint: "endpoint1",
				QueryParams: map[string]string{
					"param1": "value&with&chars",
				},
			},
			wantErr: false,
		},
		{
			name:    "url with spaces in query param keys",
			url:     "syft://test@example.com/app_data/app1/rpc/endpoint1?param with spaces=value1",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "url with special chars in query param keys",
			url:     "syft://test@example.com/app_data/app1/rpc/endpoint1?param%26with%26chars=value1",
			want:    nil,
			wantErr: true,
		},
		{
			name: "url with multiple values for same key",
			url:  "syft://test@example.com/app_data/app1/rpc/endpoint1?param1=value1&param1=value2",
			want: &SyftBoxURL{
				Datasite: "test@example.com",
				AppName:  "app1",
				Endpoint: "endpoint1",
				QueryParams: map[string]string{
					"param1": "value1",
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
				if len(tt.want.QueryParams) > 0 {
					if len(got.QueryParams) != len(tt.want.QueryParams) {
						t.Errorf("FromSyftURL() QueryParams length = %v, want %v", len(got.QueryParams), len(tt.want.QueryParams))
					}
					for k, v := range tt.want.QueryParams {
						if gotVal, exists := got.QueryParams[k]; !exists || gotVal != v {
							t.Errorf("FromSyftURL() QueryParams[%s] = %v, want %v", k, gotVal, v)
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
				Datasite: "user@example.com",
				AppName:  "app1",
				Endpoint: "endpoint1",
			},
			want: "syft://user@example.com/app_data/app1/rpc/endpoint1",
		},
		{
			name: "url with query params",
			url: &SyftBoxURL{
				Datasite: "user@example.com",
				AppName:  "app1",
				Endpoint: "endpoint1",
				QueryParams: map[string]string{
					"param1": "value1",
					"param2": "value2",
				},
			},
			want: "syft://user@example.com/app_data/app1/rpc/endpoint1?param1=value1&param2=value2",
		},
		{
			name: "url with spaces in query param values",
			url: &SyftBoxURL{
				Datasite: "user@example.com",
				AppName:  "app1",
				Endpoint: "endpoint1",
				QueryParams: map[string]string{
					"param1": "value with spaces",
				},
			},
			want: "syft://user@example.com/app_data/app1/rpc/endpoint1?param1=value+with+spaces",
		},
		{
			name: "url with special chars in query param values",
			url: &SyftBoxURL{
				Datasite: "user@example.com",
				AppName:  "app1",
				Endpoint: "endpoint1",
				QueryParams: map[string]string{
					"param1": "value&with&chars",
				},
			},
			want: "syft://user@example.com/app_data/app1/rpc/endpoint1?param1=value%26with%26chars",
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
				Datasite: "user@example.com",
				AppName:  "app1",
				Endpoint: "endpoint1",
			},
			want: "user@example.com/app_data/app1/rpc/endpoint1",
		},
		{
			name: "path with nested endpoint",
			url: &SyftBoxURL{
				Datasite: "user@example.com",
				AppName:  "app1",
				Endpoint: "endpoint1/sub/path",
			},
			want: "user@example.com/app_data/app1/rpc/endpoint1/sub/path",
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
				Datasite: "user@example.com",
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
				Datasite: "user@example.com",
				AppName:  "",
				Endpoint: "endpoint1",
			},
			wantErr: true,
		},
		{
			name: "empty endpoint",
			url: &SyftBoxURL{
				Datasite: "user@example.com",
				AppName:  "app1",
				Endpoint: "",
			},
			wantErr: true,
		},
		{
			name: "endpoint with spaces",
			url: &SyftBoxURL{
				Datasite: "user@example.com",
				AppName:  "app1",
				Endpoint: "endpoint with spaces",
			},
			wantErr: true,
		},
		{
			name: "endpoint with special chars",
			url: &SyftBoxURL{
				Datasite: "user@example.com",
				AppName:  "app1",
				Endpoint: "endpoint?with=chars",
			},
			wantErr: true,
		},
		{
			name: "invalid datasite format",
			url: &SyftBoxURL{
				Datasite: "notanemail",
				AppName:  "app1",
				Endpoint: "endpoint1",
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

func TestSyftBoxURL_SetQueryParams(t *testing.T) {
	tests := []struct {
		name        string
		url         *SyftBoxURL
		queryParams map[string]string
		want        map[string]string
	}{
		{
			name: "set query params",
			url: &SyftBoxURL{
				Datasite: "user@example.com",
				AppName:  "app1",
				Endpoint: "endpoint1",
			},
			queryParams: map[string]string{
				"param1": "value1",
				"param2": "value2",
			},
			want: map[string]string{
				"param1": "value1",
				"param2": "value2",
			},
		},
		{
			name: "set empty query params",
			url: &SyftBoxURL{
				Datasite: "user@example.com",
				AppName:  "app1",
				Endpoint: "endpoint1",
			},
			queryParams: map[string]string{},
			want:        map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.url.SetQueryParams(tt.queryParams)
			if len(tt.url.QueryParams) != len(tt.want) {
				t.Errorf("SyftBoxURL.SetQueryParams() length = %v, want %v", len(tt.url.QueryParams), len(tt.want))
				return
			}
			for k, v := range tt.want {
				if gotVal, exists := tt.url.QueryParams[k]; !exists || gotVal != v {
					t.Errorf("SyftBoxURL.SetQueryParams()[%s] = %v, want %v", k, gotVal, v)
				}
			}
		})
	}
}
